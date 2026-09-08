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
	ErrNotEditable    = stderrors.New("news: text is editable only while pending")
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
}

type CreateParams struct {
	PublisherUID int64
	SourceKey    string
	Lane         string
	Title        string
	Preview      string
	SourceURL    string
	BannerHash   string
	PublishedAt  time.Time
	WorkIDs      []int64
}

type UpdateParams struct {
	Title      *string
	Preview    *string
	SourceURL  *string
	BannerHash *string
	WorkIDs    *[]int64
}

func (s *SubmissionService) Create(ctx context.Context, p CreateParams) (Submission, error) {
	src, err := sourceRow(s.db.WithContext(ctx), p.SourceKey)
	if err != nil {
		return Submission{}, err
	}
	if src.PublisherUID != p.PublisherUID {
		return Submission{}, ErrSourceNotYours
	}
	if !src.Active {
		return Submission{}, ErrSourceInactive
	}
	if utf8.RuneCountInString(p.Preview) > model.PreviewMaxRunes {
		return Submission{}, ErrPreviewTooLong
	}
	published := p.PublishedAt
	if published.IsZero() {
		published = time.Now()
	}
	item := model.NewsItem{
		SourceKey:   src.Key,
		Lane:        p.Lane,
		ExternalID:  mintExternalID(),
		Title:       p.Title,
		Preview:     p.Preview,
		SourceURL:   p.SourceURL,
		BannerHash:  p.BannerHash,
		PublishedAt: published.UTC(),
		Status:      model.StatusPending,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return replaceWorks(tx, item.ID, p.WorkIDs)
	}); err != nil {
		return Submission{}, err
	}
	return s.load(ctx, item.ID)
}

func (s *SubmissionService) List(ctx context.Context, publisherUID, beforeID int64, limit int) ([]Submission, error) {
	q := s.db.WithContext(ctx).Model(&model.NewsItem{}).Where(ownedBySQL, publisherUID)
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
		Where(ownedBySQL, publisherUID).Count(&n).Error
	return n, err
}

func (s *SubmissionService) Get(ctx context.Context, publisherUID, itemID int64) (Submission, error) {
	var rows []model.NewsItem
	if err := s.db.WithContext(ctx).
		Where("id = ?", itemID).Where(ownedBySQL, publisherUID).
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

func (s *SubmissionService) Update(ctx context.Context, publisherUID, itemID int64, p UpdateParams) (Submission, error) {
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockOwned(tx, publisherUID, itemID)
		if err != nil {
			return err
		}
		if item.Status != model.StatusPending {
			return ErrNotEditable
		}
		src, err := sourceRow(tx, item.SourceKey)
		if err != nil {
			return err
		}
		if !src.Active {
			return ErrSourceInactive
		}
		fields := map[string]any{"updated_at": time.Now()}
		if p.Title != nil {
			fields["title"] = *p.Title
		}
		if p.Preview != nil {
			if utf8.RuneCountInString(*p.Preview) > model.PreviewMaxRunes {
				return ErrPreviewTooLong
			}
			fields["preview"] = *p.Preview
		}
		if p.SourceURL != nil {
			fields["source_url"] = *p.SourceURL
		}
		if p.BannerHash != nil {
			fields["banner_hash"] = *p.BannerHash
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
			Reason: "withdrawn by the publisher",
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

const ownedBySQL = "source_key IN (SELECT key FROM news_source WHERE publisher_uid = ?)"

func lockOwned(tx *gorm.DB, publisherUID, itemID int64) (model.NewsItem, error) {
	var rows []model.NewsItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", itemID).Where(ownedBySQL, publisherUID).
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
	names := make(map[string]string, len(sources))
	homes := make(map[string]string, len(sources))
	for _, src := range sources {
		names[src.Key] = src.DisplayName
		homes[src.Key] = src.HomepageURL
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
		out = append(out, Submission{
			ID: r.ID, SourceKey: r.SourceKey, SourceDisplayName: names[r.SourceKey],
			SourceHomepageURL: homes[r.SourceKey],
			Lane:              r.Lane, Title: r.Title, Preview: r.Preview, SourceURL: r.SourceURL,
			BannerHash: r.BannerHash, BannerURL: s.imageURL(r.BannerHash),
			PublishedAt: r.PublishedAt.UTC(), Status: r.Status,
			UpdatedAt: r.UpdatedAt.UTC(), WorkIDs: works[r.ID],
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
