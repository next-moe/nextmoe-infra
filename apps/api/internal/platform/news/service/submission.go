package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"time"
	"unicode/utf8"

	"api/internal/platform/news/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"api/pkg/imageclient"
)

var (
	ErrSourceNotYours = stderrors.New("news: source is not bound to this publisher")
	ErrSourceInactive = stderrors.New("news: source is deactivated")
	ErrPreviewTooLong = stderrors.New("news: preview exceeds the rune ceiling")
	ErrBodyTooLong    = stderrors.New("news: body exceeds the rune ceiling")
	ErrBodyNotAllowed = stderrors.New("news: only community submissions carry a body")
	ErrNotEditable    = stderrors.New("news: text is editable only while pending or published")
)

const (
	reasonTrustedSubmission  = "trusted source: published on submission (user adjudication 2026-09-29)"
	reasonEditedAfterPublish = "edited after publication; back to review"
	reasonAccountErased      = "submitter account erased"
)

// nativeExternalIDPrefix keeps API submissions out of both importers' id spaces.
// (source_key, external_id) is UNIQUE and lane is not part of it: 月幕 writes bare
// snowflake digits and galgame 批评 writes cv<id>#<ordinal>, so a publisher who is
// also bound to a partner source could otherwise mint a row that the next
// importer pass adopts and overwrites.
const nativeExternalIDPrefix = "api_"

type SubmissionService struct {
	db      *gorm.DB
	cdnBase string
}

func NewSubmissionService(db *gorm.DB, cdnBase string) *SubmissionService {
	return &SubmissionService{db: db, cdnBase: cdnBase}
}

type Submission struct {
	ID                int64
	SourceKey         string
	SourceDisplayName string
	SourceHomepageURL string
	SourceAttribution string
	SourceColumnURL   string
	Lane              string
	Title             string
	Preview           string
	SourceURL         string
	BannerHash        string
	BannerURL         string
	PublishedAt       time.Time
	Status            int16
	UpdatedAt         time.Time
	WorkIDs           []int64
	Body              string
	SubmitterUID      *int64
}

type CreateParams struct {
	PublisherUID int64
	SourceKey    string
	Lane         string
	Title        string
	Preview      string
	Body         string
	SourceURL    string
	BannerHash   string
	PublishedAt  time.Time
	WorkIDs      []int64
}

type UpdateParams struct {
	Title      *string
	Preview    *string
	Body       *string
	SourceURL  *string
	BannerHash *string
	WorkIDs    *[]int64
}

func (s *SubmissionService) Create(ctx context.Context, p CreateParams) (Submission, error) {
	src, err := sourceRow(s.db.WithContext(ctx), p.SourceKey)
	if err != nil {
		return Submission{}, err
	}
	community := src.Key == model.SourceKeyCommunity
	if !community && src.PublisherUID != p.PublisherUID {
		return Submission{}, ErrSourceNotYours
	}
	if !src.Active {
		return Submission{}, ErrSourceInactive
	}
	if err := checkText(community, p.Preview, p.Body); err != nil {
		return Submission{}, err
	}
	published := p.PublishedAt
	if published.IsZero() {
		published = time.Now()
	}
	status := model.StatusPending
	if src.AutoPublish {
		status = model.StatusPublished
	}
	uid := p.PublisherUID
	item := model.NewsItem{
		SourceKey:    src.Key,
		Lane:         p.Lane,
		ExternalID:   mintExternalID(),
		Title:        p.Title,
		Preview:      p.Preview,
		Body:         p.Body,
		SourceURL:    p.SourceURL,
		BannerHash:   p.BannerHash,
		PublishedAt:  published.UTC(),
		Status:       status,
		SubmitterUID: &uid,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		if status == model.StatusPublished {
			if err := tx.Create(&model.NewsModerationDecision{
				ItemID: item.ID, ActorUID: model.SystemActorUID,
				FromStatus: model.StatusPending, ToStatus: model.StatusPublished,
				Reason: reasonTrustedSubmission,
			}).Error; err != nil {
				return err
			}
		}
		return replaceWorks(tx, item.ID, p.WorkIDs)
	}); err != nil {
		return Submission{}, err
	}
	return s.load(ctx, item.ID)
}

func (s *SubmissionService) CountCommunitySince(ctx context.Context, uid int64, since time.Time) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.NewsItem{}).
		Where("submitter_uid = ? AND source_key = ? AND created_at > ?", uid, model.SourceKeyCommunity, since).
		Count(&n).Error
	return n, err
}

func checkText(community bool, preview, body string) error {
	if utf8.RuneCountInString(preview) > model.PreviewMaxRunes {
		return ErrPreviewTooLong
	}
	if body != "" && !community {
		return ErrBodyNotAllowed
	}
	if utf8.RuneCountInString(body) > model.BodyMaxRunes {
		return ErrBodyTooLong
	}
	return nil
}

func (s *SubmissionService) List(ctx context.Context, publisherUID, beforeID int64, limit int) ([]Submission, error) {
	q := s.db.WithContext(ctx).Model(&model.NewsItem{}).Where(ownedBySQL, publisherUID, publisherUID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.NewsItem
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return s.decorate(ctx, rows)
}

func (s *SubmissionService) Count(ctx context.Context, publisherUID int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.NewsItem{}).
		Where(ownedBySQL, publisherUID, publisherUID).Count(&n).Error
	return n, err
}

func (s *SubmissionService) Get(ctx context.Context, publisherUID, itemID int64) (Submission, error) {
	var rows []model.NewsItem
	if err := s.db.WithContext(ctx).
		Where("id = ?", itemID).Where(ownedBySQL, publisherUID, publisherUID).
		Limit(1).Find(&rows).Error; err != nil {
		return Submission{}, err
	}
	if len(rows) == 0 {
		return Submission{}, ErrNotFound
	}
	subs, err := s.decorate(ctx, rows)
	if err != nil {
		return Submission{}, err
	}
	return subs[0], nil
}

// Update edits a pending or published item. A published item leaves the feed
// for review again unless its source is trusted; either way the text a reader
// sees next has passed the same gate as a new submission.
func (s *SubmissionService) Update(ctx context.Context, publisherUID, itemID int64, p UpdateParams) (Submission, error) {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockOwned(tx, publisherUID, itemID)
		if err != nil {
			return err
		}
		if item.Status != model.StatusPending && item.Status != model.StatusPublished {
			return ErrNotEditable
		}
		src, err := sourceRow(tx, item.SourceKey)
		if err != nil {
			return err
		}
		if !src.Active {
			return ErrSourceInactive
		}
		preview, body := item.Preview, item.Body
		if p.Preview != nil {
			preview = *p.Preview
		}
		if p.Body != nil {
			body = *p.Body
		}
		if err := checkText(src.Key == model.SourceKeyCommunity, preview, body); err != nil {
			return err
		}
		fields := map[string]any{"updated_at": time.Now(), "preview": preview, "body": body}
		if p.Title != nil {
			fields["title"] = *p.Title
		}
		if p.SourceURL != nil {
			fields["source_url"] = *p.SourceURL
		}
		if p.BannerHash != nil {
			fields["banner_hash"] = *p.BannerHash
		}
		if item.Status == model.StatusPublished && !src.AutoPublish {
			fields["status"] = model.StatusPending
			if err := tx.Create(&model.NewsModerationDecision{
				ItemID: itemID, ActorUID: publisherUID, FromStatus: model.StatusPublished,
				ToStatus: model.StatusPending, Reason: reasonEditedAfterPublish,
			}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.NewsItem{}).Where("id = ?", itemID).Updates(fields).Error; err != nil {
			return err
		}
		if p.WorkIDs != nil {
			return replaceWorks(tx, itemID, *p.WorkIDs)
		}
		return nil
	}); err != nil {
		return Submission{}, err
	}
	return s.load(ctx, itemID)
}

func (s *SubmissionService) Withdraw(ctx context.Context, publisherUID, itemID int64) (Submission, error) {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockOwned(tx, publisherUID, itemID)
		if err != nil {
			return err
		}
		to, ok := transitions[ActionWithdraw][item.Status]
		if !ok {
			return fmt.Errorf("%w: withdraw from status %d", ErrIllegalTransition, item.Status)
		}
		if err := tx.Create(&model.NewsModerationDecision{
			ItemID: itemID, ActorUID: publisherUID, FromStatus: item.Status, ToStatus: to,
			Reason: "withdrawn by the submitter",
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.NewsItem{}).Where("id = ?", itemID).
			Updates(map[string]any{"status": to, "updated_at": time.Now()}).Error
	}); err != nil {
		return Submission{}, err
	}
	return s.load(ctx, itemID)
}

// ownedBySQL takes the caller's uid twice: an account owns what it submitted,
// and a partner publisher also owns everything its source imported.
const ownedBySQL = "(submitter_uid = ? OR source_key IN (SELECT key FROM news_source WHERE publisher_uid = ? AND publisher_uid <> 0))"

func lockOwned(tx *gorm.DB, publisherUID, itemID int64) (model.NewsItem, error) {
	var rows []model.NewsItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", itemID).Where(ownedBySQL, publisherUID, publisherUID).
		Limit(1).Find(&rows).Error; err != nil {
		return model.NewsItem{}, err
	}
	if len(rows) == 0 {
		return model.NewsItem{}, ErrNotFound
	}
	return rows[0], nil
}

func sourceRow(db *gorm.DB, key string) (model.NewsSource, error) {
	var rows []model.NewsSource
	if err := db.Where("key = ?", key).Limit(1).Find(&rows).Error; err != nil {
		return model.NewsSource{}, err
	}
	if len(rows) == 0 {
		return model.NewsSource{}, ErrSourceNotYours
	}
	return rows[0], nil
}

func replaceWorks(tx *gorm.DB, itemID int64, workIDs []int64) error {
	if err := tx.Where("item_id = ?", itemID).Delete(&model.NewsItemWork{}).Error; err != nil {
		return err
	}
	seen := map[int64]bool{}
	rows := make([]model.NewsItemWork, 0, len(workIDs))
	for _, id := range workIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		rows = append(rows, model.NewsItemWork{
			ItemID: itemID, WorkID: id, Confidence: model.WorkConfidenceManual,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (s *SubmissionService) load(ctx context.Context, itemID int64) (Submission, error) {
	var rows []model.NewsItem
	if err := s.db.WithContext(ctx).Where("id = ?", itemID).Limit(1).Find(&rows).Error; err != nil {
		return Submission{}, err
	}
	if len(rows) == 0 {
		return Submission{}, ErrNotFound
	}
	subs, err := s.decorate(ctx, rows)
	if err != nil {
		return Submission{}, err
	}
	return subs[0], nil
}

func (s *SubmissionService) decorate(ctx context.Context, rows []model.NewsItem) ([]Submission, error) {
	out := make([]Submission, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(rows))
	keys := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		ids = append(ids, r.ID)
		if !seen[r.SourceKey] {
			seen[r.SourceKey] = true
			keys = append(keys, r.SourceKey)
		}
	}
	var sources []model.NewsSource
	if err := s.db.WithContext(ctx).Where("key IN ?", keys).Find(&sources).Error; err != nil {
		return nil, err
	}
	byKey := make(map[string]model.NewsSource, len(sources))
	for _, src := range sources {
		byKey[src.Key] = src
	}
	var links []model.NewsItemWork
	if err := s.db.WithContext(ctx).Where("item_id IN ?", ids).
		Order("item_id, work_id").Find(&links).Error; err != nil {
		return nil, err
	}
	works := make(map[int64][]int64, len(ids))
	for _, l := range links {
		works[l.ItemID] = append(works[l.ItemID], l.WorkID)
	}
	for _, r := range rows {
		src := byKey[r.SourceKey]
		out = append(out, Submission{
			ID: r.ID, SourceKey: r.SourceKey, SourceDisplayName: src.DisplayName,
			SourceHomepageURL: src.HomepageURL, SourceAttribution: src.Attribution,
			SourceColumnURL: src.ColumnURL,
			Lane:            r.Lane, Title: r.Title, Preview: r.Preview, SourceURL: r.SourceURL,
			BannerHash: r.BannerHash, BannerURL: s.imageURL(r.BannerHash),
			PublishedAt: r.PublishedAt.UTC(), Status: r.Status,
			UpdatedAt: r.UpdatedAt.UTC(), WorkIDs: works[r.ID],
			Body: r.Body, SubmitterUID: r.SubmitterUID,
		})
	}
	return out, nil
}

func mintExternalID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return nativeExternalIDPrefix + hex.EncodeToString(raw[:])
}

func (s *SubmissionService) imageURL(hash string) string {
	if hash == "" || s.cdnBase == "" {
		return ""
	}
	return imageclient.MainURL(s.cdnBase, hash, "webp")
}
