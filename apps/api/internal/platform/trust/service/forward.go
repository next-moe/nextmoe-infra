package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/trust/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ForwardService struct {
	db        *gorm.DB
	allowlist map[string]bool
}

func NewForwardService(db *gorm.DB, allowlist map[string]bool) *ForwardService {
	return &ForwardService{db: db, allowlist: allowlist}
}

func (s *ForwardService) allowed(clientID string) bool {
	return clientID != "" && s.allowlist[clientID]
}

type ForwardParams struct {
	CallerClientID string
	Site           string
	SubjectKind    string
	SubjectID      string
	Severity       *int16
	WeightSum      *float32
	ContextNote    *string
	SubjectReach   *int64
	AuthorID       *int64
}

type ForwardResult struct {
	ReviewItemID int64
	Created      bool
}

func (s *ForwardService) Forward(ctx context.Context, p ForwardParams) (ForwardResult, error) {
	if !s.allowed(p.CallerClientID) {
		return ForwardResult{}, ErrForwarderNotAllowed
	}

	var kindCount int64
	if err := s.db.WithContext(ctx).Model(&model.TrustSubjectKind{}).
		Where("site = ? AND key = ? AND is_deprecated = false", p.Site, p.SubjectKind).
		Count(&kindCount).Error; err != nil {
		return ForwardResult{}, err
	}
	if kindCount == 0 {
		return ForwardResult{}, ErrSubjectKindNotRegistered
	}

	var result ForwardResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var open model.TrustReviewItem
		lerr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
				p.Site, p.SubjectKind, p.SubjectID, []int16{model.ReviewStatusPending, model.ReviewStatusClaimed}).
			Limit(1).Take(&open).Error
		if lerr == nil {
			updates := map[string]any{}
			if p.WeightSum != nil {
				updates["report_weight_sum"] = gorm.Expr("GREATEST(COALESCE(report_weight_sum, 0), ?)", *p.WeightSum)
			}
			if p.ContextNote != nil {
				updates["context_note"] = gorm.Expr(
					"CASE WHEN context_note IS NULL OR context_note = '' THEN ? ELSE context_note END", *p.ContextNote)
			}
			if p.AuthorID != nil {
				updates["subject_author_id"] = gorm.Expr("COALESCE(subject_author_id, ?)", *p.AuthorID)
			}
			if reach := maxReach(open.SubjectReach, p.SubjectReach); reach != nil {
				updates["subject_reach"] = *reach
				updates["priority"] = repriceForReach(open.Priority, open.SubjectReach, reach)
			}
			if len(updates) > 0 {
				if err := tx.Model(&model.TrustReviewItem{}).Where("id = ?", open.ID).Updates(updates).Error; err != nil {
					return err
				}
			}
			result = ForwardResult{ReviewItemID: open.ID, Created: false}
			return nil
		} else if !errors.Is(lerr, gorm.ErrRecordNotFound) {
			return lerr
		}

		item := model.TrustReviewItem{
			Site: p.Site, SubjectKind: p.SubjectKind, SubjectID: p.SubjectID,
			SubjectAuthorID: p.AuthorID, Source: model.ReviewSourceCommunityForward, Severity: p.Severity,
			ReportWeightSum: p.WeightSum, ContextNote: p.ContextNote,
			SubjectReach: p.SubjectReach,
			Priority:     rankPriority(forwardPriority(p.Severity), p.SubjectReach),
			Status:       model.ReviewStatusPending,
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
				p.Site, p.SubjectKind, p.SubjectID, []int16{model.ReviewStatusPending, model.ReviewStatusClaimed}).
				Limit(1).Take(&item).Error; err != nil {
				return err
			}
			if err := adoptSubjectAuthor(tx, item.ID, p.AuthorID); err != nil {
				return err
			}
			result = ForwardResult{ReviewItemID: item.ID, Created: false}
			return nil
		}
		result = ForwardResult{ReviewItemID: item.ID, Created: true}
		return nil
	})
	if err != nil {
		return ForwardResult{}, err
	}
	return result, nil
}

type ResolveParams struct {
	CallerClientID string
	ReviewItemID   int64
	Outcome        string
	ActorRef       *string
}

type ResolveResult struct {
	Closed bool
}

func (s *ForwardService) Resolve(ctx context.Context, p ResolveParams) (ResolveResult, error) {
	if !s.allowed(p.CallerClientID) {
		return ResolveResult{}, ErrForwarderNotAllowed
	}
	if p.Outcome != "approved" && p.Outcome != "rejected" {
		return ResolveResult{}, ErrInvalidOutcome
	}

	var result ResolveResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.TrustReviewItem
		lerr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&item, p.ReviewItemID).Error
		if errors.Is(lerr, gorm.ErrRecordNotFound) {
			return ErrReviewItemNotFound
		}
		if lerr != nil {
			return lerr
		}
		if item.Status != model.ReviewStatusPending && item.Status != model.ReviewStatusClaimed {
			result = ResolveResult{Closed: false}
			return nil
		}

		status := model.ReviewStatusActioned
		if p.Outcome == "approved" {
			status = model.ReviewStatusDismissed
		}
		if err := tx.Model(&model.TrustReviewItem{}).Where("id = ?", p.ReviewItemID).
			Updates(map[string]any{
				"status":     status,
				"decided_at": time.Now(),
			}).Error; err != nil {
			return err
		}
		result = ResolveResult{Closed: true}
		return AppendAudit(tx, AuditEntry{
			Action: "review_resolved_by_site",
			Site:   strptr(item.Site), SubjectKind: strptr(item.SubjectKind), SubjectID: strptr(item.SubjectID),
			PolicyRef: p.ActorRef,
		})
	})
	if err != nil {
		return ResolveResult{}, err
	}
	return result, nil
}

func forwardPriority(severity *int16) float32 {
	if severity != nil {
		return float32(*severity)
	}
	return 1
}
