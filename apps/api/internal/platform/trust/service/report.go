package service

import (
	"context"
	"net/url"
	"time"

	"api/internal/platform/settings/keys"
	"api/internal/platform/trust/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ReportService struct {
	db      *gorm.DB
	weigher Weigher
	policy  *PolicyService
}

func NewReportService(db *gorm.DB, weigher Weigher, opts ...ReportServiceOption) *ReportService {
	s := &ReportService{db: db, weigher: weigher}
	for _, o := range opts {
		o(s)
	}
	return s
}

type ReportServiceOption func(*ReportService)

func WithReportPolicy(p *PolicyService) ReportServiceOption {
	return func(s *ReportService) { s.policy = p }
}

func (s *ReportService) aggregateThresholdFor(site string) float32 {
	if s.policy == nil {
		return DefaultAggregateThreshold()
	}
	return s.policy.Resolve(site).AggregateThreshold
}

type ReportParams struct {
	Site        string
	SubjectKind string
	SubjectID   string
	ReasonKey   string
	Note        *string
	Snapshot    *string
	SubjectURL  *string
	AuthorID    *int64
	ReporterID  int64
}

type ReportResult struct {
	ReportID     int64
	ReviewItemID *int64
}

func (s *ReportService) Submit(ctx context.Context, p ReportParams) (ReportResult, error) {
	if err := validateSubjectURL(p.SubjectURL); err != nil {
		return ReportResult{}, err
	}

	var kindCount int64
	if err := s.db.WithContext(ctx).Model(&model.TrustSubjectKind{}).
		Where("site = ? AND key = ? AND is_deprecated = false", p.Site, p.SubjectKind).
		Count(&kindCount).Error; err != nil {
		return ReportResult{}, err
	}
	if kindCount == 0 {
		return ReportResult{}, ErrSubjectKindNotRegistered
	}

	var reason struct {
		ID       int64
		Severity int16
	}
	if err := s.db.WithContext(ctx).Model(&model.TrustReportReason{}).
		Select("id, severity").
		Where("key = ? AND (site = ? OR site IS NULL) AND is_deprecated = false", p.ReasonKey, p.Site).
		Order("site NULLS LAST").Limit(1).Scan(&reason).Error; err != nil {
		return ReportResult{}, err
	}
	if reason.ID == 0 {
		return ReportResult{}, ErrReasonUnknown
	}

	var recent int64
	if err := s.db.WithContext(ctx).Model(&model.TrustReport{}).
		Where("reporter_id = ? AND created_at > ?", p.ReporterID, time.Now().Add(-time.Duration(keys.TrustReportRateWindowMinutes.Get())*time.Minute)).
		Count(&recent).Error; err != nil {
		return ReportResult{}, err
	}
	if recent >= keys.TrustReportRateMaxPerWindow.Get() {
		return ReportResult{}, ErrRateLimited
	}

	weight, err := s.weigher.Weigh(ctx, p.ReporterID)
	if err != nil {
		return ReportResult{}, err
	}

	var result ReportResult
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		report := model.TrustReport{
			Site: p.Site, SubjectKind: p.SubjectKind, SubjectID: p.SubjectID,
			ReporterID: p.ReporterID, ReasonID: reason.ID, Note: p.Note,
			SubjectSnapshot: p.Snapshot, SubjectURL: p.SubjectURL, SubjectAuthorID: p.AuthorID,
			Weight: weight.Weight, Status: model.ReportStatusReceived,
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&report)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var existing model.TrustReport
			if err := tx.Where(
				"site = ? AND subject_kind = ? AND subject_id = ? AND reporter_id = ?",
				p.Site, p.SubjectKind, p.SubjectID, p.ReporterID,
			).Take(&existing).Error; err != nil {
				return err
			}
			result = ReportResult{ReportID: existing.ID, ReviewItemID: existing.ReviewItemID}
			return nil
		}

		result.ReportID = report.ID
		linked, err := s.aggregate(tx, p, report.ID, weight, reason.Severity)
		if err != nil {
			return err
		}
		result.ReviewItemID = linked
		return nil
	})
	if txErr != nil {
		return ReportResult{}, txErr
	}
	return result, nil
}

func (s *ReportService) aggregate(tx *gorm.DB, p ReportParams, reportID int64, weight ReporterWeight, severity int16) (*int64, error) {
	var open model.TrustReviewItem
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
			p.Site, p.SubjectKind, p.SubjectID, []int16{model.ReviewStatusPending, model.ReviewStatusClaimed}).
		Limit(1).Take(&open).Error
	if err == nil {
		if err := s.linkReport(tx, reportID, open.ID); err != nil {
			return nil, err
		}
		if err := tx.Model(&model.TrustReviewItem{}).Where("id = ?", open.ID).
			Update("report_weight_sum", gorm.Expr("COALESCE(report_weight_sum, 0) + ?", weight.Weight)).Error; err != nil {
			return nil, err
		}
		if err := adoptSubjectAuthor(tx, open.ID, p.AuthorID); err != nil {
			return nil, err
		}
		return &open.ID, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	var dismissed model.TrustReviewItem
	err = tx.Where("site = ? AND subject_kind = ? AND subject_id = ? AND status = ? AND decided_at > ?",
		p.Site, p.SubjectKind, p.SubjectID, model.ReviewStatusDismissed, time.Now().Add(-foldWindow)).
		Order("decided_at DESC").Limit(1).Take(&dismissed).Error
	if err == nil {
		if err := tx.Model(&model.TrustReport{}).Where("id = ?", reportID).
			Updates(map[string]any{"review_item_id": dismissed.ID, "status": model.ReportStatusFolded}).Error; err != nil {
			return nil, err
		}
		if err := adoptSubjectAuthor(tx, dismissed.ID, p.AuthorID); err != nil {
			return nil, err
		}
		return &dismissed.ID, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	unlinked := func() *gorm.DB {
		return tx.Model(&model.TrustReport{}).
			Where("site = ? AND subject_kind = ? AND subject_id = ? AND status <> ? AND review_item_id IS NULL",
				p.Site, p.SubjectKind, p.SubjectID, model.ReportStatusFolded)
	}
	var sum float32
	if err := unlinked().Select("COALESCE(SUM(weight), 0)").Scan(&sum).Error; err != nil {
		return nil, err
	}
	if !weight.Staff && sum < s.aggregateThresholdFor(p.Site) {
		return nil, nil
	}
	var authors []int64
	if err := unlinked().Where("subject_author_id IS NOT NULL").
		Order("id").Limit(1).Pluck("subject_author_id", &authors).Error; err != nil {
		return nil, err
	}
	var author *int64
	if len(authors) > 0 {
		author = &authors[0]
	}

	item := model.TrustReviewItem{
		Site: p.Site, SubjectKind: p.SubjectKind, SubjectID: p.SubjectID,
		SubjectAuthorID: author, Source: model.ReviewSourceReports, Severity: &severity,
		ReportWeightSum: &sum,
		Priority:        rankPriority(float32(severity), nil),
		Status:          model.ReviewStatusPending,
	}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		if err := tx.Where("site = ? AND subject_kind = ? AND subject_id = ? AND status IN ?",
			p.Site, p.SubjectKind, p.SubjectID, []int16{model.ReviewStatusPending, model.ReviewStatusClaimed}).
			Limit(1).Take(&item).Error; err != nil {
			return nil, err
		}
		if err := adoptSubjectAuthor(tx, item.ID, author); err != nil {
			return nil, err
		}
	}
	if err := unlinked().
		Updates(map[string]any{"review_item_id": item.ID, "status": model.ReportStatusLinked}).Error; err != nil {
		return nil, err
	}
	return &item.ID, nil
}

func validateSubjectURL(raw *string) error {
	if raw == nil || *raw == "" {
		return nil
	}
	if len(*raw) > 512 {
		return ErrInvalidSubjectURL
	}
	u, err := url.Parse(*raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidSubjectURL
	}
	return nil
}

func adoptSubjectAuthor(tx *gorm.DB, itemID int64, author *int64) error {
	if author == nil {
		return nil
	}
	return tx.Model(&model.TrustReviewItem{}).
		Where("id = ? AND subject_author_id IS NULL", itemID).
		Update("subject_author_id", *author).Error
}

func (s *ReportService) linkReport(tx *gorm.DB, reportID, itemID int64) error {
	return tx.Model(&model.TrustReport{}).Where("id = ?", reportID).
		Updates(map[string]any{"review_item_id": itemID, "status": model.ReportStatusLinked}).Error
}
