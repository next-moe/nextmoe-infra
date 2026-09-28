package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/trust/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ReviewService struct{ db *gorm.DB }

func NewReviewService(db *gorm.DB) *ReviewService { return &ReviewService{db: db} }

type ReviewFilters struct {
	Site            string
	Status          *int16
	Source          *int16
	SubjectAuthorID int64
	Page            int
	Limit           int
}

func (s *ReviewService) List(ctx context.Context, f ReviewFilters) ([]model.TrustReviewItem, int64, error) {
	q := s.db.WithContext(ctx).Model(&model.TrustReviewItem{})
	if f.Site != "" {
		q = q.Where("site = ?", f.Site)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.Source != nil {
		q = q.Where("source = ?", *f.Source)
	}
	if f.SubjectAuthorID != 0 {
		q = q.Where("subject_author_id = ?", f.SubjectAuthorID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := clampLimit(f.Limit)
	offset := 0
	if f.Page > 1 {
		offset = (f.Page - 1) * limit
	}
	var items []model.TrustReviewItem
	if err := q.Order("priority DESC").Order("id DESC").
		Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *ReviewService) Get(ctx context.Context, id int64, siteScope string) (*model.TrustReviewItem, []model.TrustReport, error) {
	var item model.TrustReviewItem
	q := s.db.WithContext(ctx)
	if siteScope != "" {
		q = q.Where("site = ?", siteScope)
	}
	err := q.Take(&item, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrReviewItemNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	var reports []model.TrustReport
	if err := s.db.WithContext(ctx).Where("review_item_id = ?", id).
		Order("id ASC").Find(&reports).Error; err != nil {
		return nil, nil, err
	}
	return &item, reports, nil
}

func (s *ReviewService) Claim(ctx context.Context, id, claimedBy int64, siteScope string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.TrustReviewItem
		lockQ := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND status = ?", id, model.ReviewStatusPending)
		if siteScope != "" {
			lockQ = lockQ.Where("site = ?", siteScope)
		}
		err := lockQ.Take(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			existsQ := tx.Model(&model.TrustReviewItem{}).Where("id = ?", id)
			if siteScope != "" {
				existsQ = existsQ.Where("site = ?", siteScope)
			}
			var exists int64
			if cerr := existsQ.Count(&exists).Error; cerr != nil {
				return cerr
			}
			if exists == 0 {
				return ErrReviewItemNotFound
			}
			return ErrAlreadyClaimed
		}
		if err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(&model.TrustReviewItem{}).Where("id = ?", id).
			Updates(map[string]any{
				"status":     model.ReviewStatusClaimed,
				"claimed_by": claimedBy,
				"claimed_at": now,
			}).Error
	})
}

type DecideParams struct {
	ID         int64
	DecidedBy  int64
	Decision   string
	Action     *int16
	ReasonCode string
	Statement  *string
	SiteScope  string
}

func (s *ReviewService) Decide(ctx context.Context, p DecideParams) (*int64, error) {
	if p.Decision != "dismissed" && p.Decision != "actioned" {
		return nil, ErrInvalidDecision
	}
	if p.Decision == "actioned" && (p.Action == nil || p.ReasonCode == "") {
		return nil, ErrInvalidDecision
	}

	var dispositionID *int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.TrustReviewItem
		lockQ := tx.Clauses(clause.Locking{Strength: "UPDATE"})
		if p.SiteScope != "" {
			lockQ = lockQ.Where("site = ?", p.SiteScope)
		}
		lerr := lockQ.Take(&item, p.ID).Error
		if errors.Is(lerr, gorm.ErrRecordNotFound) {
			return ErrReviewItemNotFound
		}
		if lerr != nil {
			return lerr
		}
		if item.Status != model.ReviewStatusPending && item.Status != model.ReviewStatusClaimed {
			return ErrIllegalTransition
		}

		now := time.Now()
		if p.Decision == "dismissed" {
			if err := tx.Model(&model.TrustReviewItem{}).Where("id = ?", p.ID).
				Updates(map[string]any{
					"status":     model.ReviewStatusDismissed,
					"decided_by": p.DecidedBy,
					"decided_at": now,
				}).Error; err != nil {
				return err
			}
			var kind model.TrustSubjectKind
			kerr := tx.Where("site = ? AND key = ?", item.Site, item.SubjectKind).Take(&kind).Error
			if kerr != nil && !errors.Is(kerr, gorm.ErrRecordNotFound) {
				return kerr
			}
			if kind.NotifyOnDismiss && kind.CallbackURL != nil && *kind.CallbackURL != "" {
				pending := model.CallbackStatusPending
				disp := model.TrustDisposition{
					ReviewItemID: item.ID, Action: model.ActionNone, ActedBy: p.DecidedBy,
					ReasonCode: "review_dismissed", CallbackStatus: &pending, NextAttemptAt: &now,
				}
				if err := tx.Create(&disp).Error; err != nil {
					return err
				}
				dispositionID = &disp.ID
			}
			return AppendAudit(tx, AuditEntry{
				ActorID: &p.DecidedBy, Action: "review_dismissed",
				Site: strptr(item.Site), SubjectKind: strptr(item.SubjectKind), SubjectID: strptr(item.SubjectID),
			})
		}

		if err := tx.Model(&model.TrustReviewItem{}).Where("id = ?", p.ID).
			Updates(map[string]any{
				"status":     model.ReviewStatusActioned,
				"decided_by": p.DecidedBy,
				"decided_at": now,
			}).Error; err != nil {
			return err
		}

		var kind model.TrustSubjectKind
		kerr := tx.Where("site = ? AND key = ?", item.Site, item.SubjectKind).Take(&kind).Error
		if kerr != nil && !errors.Is(kerr, gorm.ErrRecordNotFound) {
			return kerr
		}
		disp := model.TrustDisposition{
			ReviewItemID: item.ID, Action: *p.Action, ActedBy: p.DecidedBy,
			ReasonCode: p.ReasonCode, Statement: p.Statement,
		}
		if kind.CallbackURL != nil && *kind.CallbackURL != "" {
			pending := model.CallbackStatusPending
			disp.CallbackStatus = &pending
			disp.NextAttemptAt = &now
		}
		if err := tx.Create(&disp).Error; err != nil {
			return err
		}
		dispositionID = &disp.ID
		return AppendAudit(tx, AuditEntry{
			ActorID: &p.DecidedBy, Action: "review_actioned",
			Site: strptr(item.Site), SubjectKind: strptr(item.SubjectKind), SubjectID: strptr(item.SubjectID),
			ReasonCode: strptr(p.ReasonCode),
		})
	})
	if err != nil {
		return nil, err
	}
	return dispositionID, nil
}

func clampLimit(limit int) int {
	const def = 50
	if limit <= 0 || limit > 200 {
		return def
	}
	return limit
}
