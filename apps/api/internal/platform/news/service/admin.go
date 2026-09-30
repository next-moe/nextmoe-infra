package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"api/internal/platform/news/dto"
	"api/internal/platform/news/model"
	"api/pkg/imageclient"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUnknownAction     = errors.New("unknown action")
	ErrIllegalTransition = errors.New("illegal transition")
)

// Actions are named for what a moderator does, not for the status they land on:
// "withdraw" and "reject" both end in a hidden item, but they are different
// events and the audit line must keep them apart.
const (
	ActionPublish  = "publish"
	ActionReject   = "reject"
	ActionWithdraw = "withdraw"
	ActionRepend   = "repend"
)

// transitions is the whole write contract of the admin face. Appeals are
// deliberately open: Tier0 auto-rejects and mistaken withdrawals must have a way
// back, or an automated decision becomes permanent by accident.
var transitions = map[string]map[int16]int16{
	ActionPublish: {
		model.StatusPending:   model.StatusPublished,
		model.StatusRejected:  model.StatusPublished,
		model.StatusWithdrawn: model.StatusPublished,
	},
	ActionReject: {
		model.StatusPending:   model.StatusRejected,
		model.StatusWithdrawn: model.StatusRejected,
	},
	ActionWithdraw: {
		model.StatusPublished: model.StatusWithdrawn,
	},
	ActionRepend: {
		model.StatusPublished: model.StatusPending,
		model.StatusRejected:  model.StatusPending,
		model.StatusWithdrawn: model.StatusPending,
	},
}

type AdminService struct {
	db      *gorm.DB
	cdnBase string
}

func NewAdminService(db *gorm.DB, cdnBase string) *AdminService {
	return &AdminService{db: db, cdnBase: cdnBase}
}

type QueueFilter struct {
	Statuses []int16
	Lanes    []string
	Sources  []string
	// Ungraded selects items whose current text has no usable verdict — the
	// backlog a moderator cannot lean on the machine for.
	Ungraded bool
	// Degraded selects items whose newest verdict for the current text exists but
	// could not actually judge.
	Degraded bool
}

func (s *AdminService) Queue(ctx context.Context, f QueueFilter, offset, limit int) (dto.AdminNewsQueueData, error) {
	q := s.db.WithContext(ctx).Model(&model.NewsItem{})
	if len(f.Statuses) > 0 {
		q = q.Where("status IN ?", f.Statuses)
	}
	if len(f.Lanes) > 0 {
		q = q.Where("lane IN ?", f.Lanes)
	}
	if len(f.Sources) > 0 {
		q = q.Where("source_key IN ?", f.Sources)
	}

	var out dto.AdminNewsQueueData
	q = q.Order("published_at DESC, id DESC")

	// The grading filters cannot run in SQL: "is this verdict current" compares a
	// digest of the item's own text, which is not a column. They are also the
	// reason the console can separate "nobody has looked at this" from "the
	// machine looked and had nothing to say" — collapsing those two into one
	// status is how a queue stops being one.
	//
	// So when one is set the page is cut AFTER filtering, over the whole matching
	// set rather than one SQL page. Paginating first would leave later pages
	// arbitrarily short, and the total would count rows the caller filtered out.
	// The set is bounded by the status filter and the table's low-thousands
	// ceiling (00-workflow §2).
	if !f.Ungraded && !f.Degraded {
		if err := q.Count(&out.Total).Error; err != nil {
			return out, err
		}
		var rows []model.NewsItem
		if err := q.Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
			return out, err
		}
		views, err := s.decorate(ctx, rows)
		if err != nil {
			return out, err
		}
		out.Items = views
		return out, nil
	}

	var rows []model.NewsItem
	if err := q.Find(&rows).Error; err != nil {
		return out, err
	}
	views, err := s.decorate(ctx, rows)
	if err != nil {
		return out, err
	}
	kept := make([]dto.AdminNewsItem, 0, len(views))
	for _, v := range views {
		switch {
		case f.Ungraded && v.Verdict == nil:
			kept = append(kept, v)
		case f.Degraded && v.Verdict != nil && v.Verdict.Degraded:
			kept = append(kept, v)
		}
	}
	out.Total = int64(len(kept))
	if offset >= len(kept) {
		out.Items = []dto.AdminNewsItem{}
		return out, nil
	}
	end := min(offset+limit, len(kept))
	out.Items = kept[offset:end]
	return out, nil
}

func (s *AdminService) Item(ctx context.Context, id int64) (dto.AdminNewsItemDetail, error) {
	var out dto.AdminNewsItemDetail
	var rows []model.NewsItem
	if err := s.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
		return out, err
	}
	if len(rows) == 0 {
		return out, ErrNotFound
	}
	views, err := s.decorate(ctx, rows)
	if err != nil {
		return out, err
	}
	out.AdminNewsItem = views[0]
	out.Body = rows[0].Body

	var verdicts []model.NewsModerationVerdict
	if err := s.db.WithContext(ctx).Where("item_id = ?", id).
		Order("id DESC").Find(&verdicts).Error; err != nil {
		return out, err
	}
	fp := rows[0].Fingerprint()
	out.Verdicts = make([]dto.AdminVerdict, 0, len(verdicts))
	for _, v := range verdicts {
		out.Verdicts = append(out.Verdicts, verdictView(v, fp))
	}

	var decisions []model.NewsModerationDecision
	if err := s.db.WithContext(ctx).Where("item_id = ?", id).
		Order("id DESC").Find(&decisions).Error; err != nil {
		return out, err
	}
	out.Decisions = make([]dto.AdminDecision, 0, len(decisions))
	for _, d := range decisions {
		out.Decisions = append(out.Decisions, dto.AdminDecision{
			ID: d.ID, ActorUID: d.ActorUID, FromStatus: d.FromStatus,
			ToStatus: d.ToStatus, Reason: d.Reason, CreatedAt: d.CreatedAt,
		})
	}
	return out, nil
}

// Decide is the only write on the admin face. Four separate endpoints would each
// need this same guard and this same audit row, and one of the four copies would
// eventually drift.
func (s *AdminService) Decide(ctx context.Context, id int64, actorUID int64, action, reason string) (dto.AdminNewsItemDetail, error) {
	allowed, ok := transitions[action]
	if !ok {
		return dto.AdminNewsItemDetail{}, ErrUnknownAction
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.NewsItem
		// Lock the row: two moderators opening the same queue is the normal case,
		// and without this both reads could see pending and both write an audit
		// line claiming they were the one who moved it.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrNotFound
		}
		from := rows[0].Status
		to, ok := allowed[from]
		if !ok {
			return fmt.Errorf("%w: %s from status %d", ErrIllegalTransition, action, from)
		}
		if err := tx.Create(&model.NewsModerationDecision{
			ItemID: id, ActorUID: actorUID, FromStatus: from, ToStatus: to, Reason: reason,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.NewsItem{}).Where("id = ?", id).
			Updates(map[string]any{"status": to, "updated_at": time.Now()}).Error
	})
	if err != nil {
		return dto.AdminNewsItemDetail{}, err
	}
	return s.Item(ctx, id)
}

func (s *AdminService) Stats(ctx context.Context) (dto.AdminNewsStatsData, error) {
	out := dto.AdminNewsStatsData{
		ByStatus: map[string]int64{},
		ByLane:   map[string]int64{},
	}
	type pair struct {
		K string
		N int64
	}
	var byStatus []pair
	if err := s.db.WithContext(ctx).Model(&model.NewsItem{}).
		Select("status::text AS k, count(*) AS n").Group("status").Scan(&byStatus).Error; err != nil {
		return out, err
	}
	for _, p := range byStatus {
		out.ByStatus[p.K] = p.N
	}
	var byLane []pair
	if err := s.db.WithContext(ctx).Model(&model.NewsItem{}).
		Select("lane AS k, count(*) AS n").Group("lane").Scan(&byLane).Error; err != nil {
		return out, err
	}
	for _, p := range byLane {
		out.ByLane[p.K] = p.N
	}

	var pending []model.NewsItem
	if err := s.db.WithContext(ctx).
		Where("status = ? AND dead_at IS NULL", model.StatusPending).Find(&pending).Error; err != nil {
		return out, err
	}
	views, err := s.decorate(ctx, pending)
	if err != nil {
		return out, err
	}
	for _, v := range views {
		switch {
		case v.Verdict == nil:
			out.Ungraded++
		case v.Verdict.Degraded:
			out.Degraded++
		}
	}
	return out, nil
}

// decorate attaches the newest verdict that judged each item's CURRENT text.
// A verdict for older text is history, not a judgement of what is on the page,
// and a degraded one never counts as a judgement at all.
func (s *AdminService) decorate(ctx context.Context, rows []model.NewsItem) ([]dto.AdminNewsItem, error) {
	out := make([]dto.AdminNewsItem, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	var verdicts []model.NewsModerationVerdict
	if err := s.db.WithContext(ctx).Where("item_id IN ?", ids).
		Order("id DESC").Find(&verdicts).Error; err != nil {
		return nil, err
	}
	byItem := map[int64][]model.NewsModerationVerdict{}
	for _, v := range verdicts {
		byItem[v.ItemID] = append(byItem[v.ItemID], v)
	}

	for _, r := range rows {
		fp := r.Fingerprint()
		view := dto.AdminNewsItem{
			ID: r.ID, SourceKey: r.SourceKey, Lane: r.Lane,
			UpstreamCategory: r.UpstreamCategory, ExternalID: r.ExternalID,
			Title: r.Title, Preview: r.Preview, SourceURL: r.SourceURL,
			BannerURL:   s.imageURL(r.BannerHash),
			PublishedAt: r.PublishedAt, Status: r.Status, DeadAt: r.DeadAt,
			FirstSeenAt: r.FirstSeenAt, LastSeenAt: r.LastSeenAt,
			SubmitterUID: r.SubmitterUID,
		}
		for _, v := range byItem[r.ID] {
			if v.ContentFingerprint != fp {
				continue
			}
			view.Attempts++
			if view.Verdict == nil {
				vv := verdictView(v, fp)
				view.Verdict = &vv
			}
		}
		out = append(out, view)
	}
	return out, nil
}

func verdictView(v model.NewsModerationVerdict, currentFP string) dto.AdminVerdict {
	return dto.AdminVerdict{
		ID: v.ID, ContentFingerprint: v.ContentFingerprint,
		Tier0Decision: v.Tier0Decision, Tier0Matched: jsonStrings(v.Tier0Matched),
		AIFlagged: v.AIFlagged, AIScore: v.AIScore, AICategories: jsonStrings(v.AICategories),
		AIChannel: v.AIChannel, Degraded: v.Degraded, DegradedReason: v.DegradedReason,
		CreatedAt: v.CreatedAt, Current: v.ContentFingerprint == currentFP,
	}
}

func jsonStrings(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func (s *AdminService) imageURL(hash string) string {
	if hash == "" || s.cdnBase == "" {
		return ""
	}
	return imageclient.MainURL(s.cdnBase, hash, "webp")
}
