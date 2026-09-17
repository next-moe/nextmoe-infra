package repository

import (
	"context"
	"time"

	"api/internal/platform/image/model"

	"gorm.io/gorm"
)

type StatsRepository struct {
	db *gorm.DB
}

func NewStatsRepository(db *gorm.DB) *StatsRepository {
	return &StatsRepository{db: db}
}

type StatsResult struct {
	UploadCount       int64                `json:"upload_count"`
	UniqueImages      int64                `json:"unique_images"`
	DeduplicatedCount int64                `json:"deduplicated_count"`
	TotalBytes        int64                `json:"total_bytes"`
	BySite            map[string]SiteStats `json:"by_site,omitempty"`
	ReviewPending     int64                `json:"review_pending"`
	ReviewRejected    int64                `json:"review_rejected"`
}

type SiteStats struct {
	Count  int64 `json:"count"`
	Unique int64 `json:"unique"`
}

type ScopeFilter struct {
	Site string
	From time.Time
	To   time.Time
}

func (r *StatsRepository) Stats(ctx context.Context, f ScopeFilter) (*StatsResult, error) {
	out := &StatsResult{BySite: map[string]SiteStats{}}

	usageQ := r.db.WithContext(ctx).Model(&model.ImageSiteUsage{})
	if f.Site != "" {
		usageQ = usageQ.Where("site = ?", f.Site)
	}
	if !f.From.IsZero() {
		usageQ = usageQ.Where("first_uploaded_at >= ?", f.From)
	}
	if !f.To.IsZero() {
		usageQ = usageQ.Where("first_uploaded_at <= ?", f.To)
	}

	usageQ = usageQ.Where("hash IN (?)",
		r.db.Model(&model.Image{}).Select("hash").Where("deleted_at IS NULL"))

	type usageAgg struct {
		TotalCount  int64
		UniqueCount int64
	}
	var agg usageAgg
	if err := usageQ.
		Select("COALESCE(SUM(upload_count),0) AS total_count, COUNT(*) AS unique_count").
		Scan(&agg).Error; err != nil {
		return nil, err
	}
	out.UploadCount = agg.TotalCount
	out.UniqueImages = agg.UniqueCount
	out.DeduplicatedCount = agg.TotalCount - agg.UniqueCount

	bytesRow := r.db.WithContext(ctx).
		Model(&model.Image{}).
		Where("deleted_at IS NULL")
	if f.Site != "" {
		bytesRow = bytesRow.Where("hash IN (?)",
			r.db.Model(&model.ImageSiteUsage{}).Select("hash").Where("site = ?", f.Site))
	}
	if err := bytesRow.Select("COALESCE(SUM(size_bytes),0)").Scan(&out.TotalBytes).Error; err != nil {
		return nil, err
	}

	if f.Site == "" {
		if err := r.db.WithContext(ctx).Model(&model.Image{}).
			Where("review_status = ? AND deleted_at IS NULL", model.ReviewPending).
			Count(&out.ReviewPending).Error; err != nil {
			return nil, err
		}
		if err := r.db.WithContext(ctx).Model(&model.Image{}).
			Where("review_status = ? AND deleted_at IS NULL", model.ReviewRejected).
			Count(&out.ReviewRejected).Error; err != nil {
			return nil, err
		}

		type row struct {
			Site   string
			Count  int64
			Unique int64
		}
		var rows []row
		if err := r.db.WithContext(ctx).Model(&model.ImageSiteUsage{}).
			Where("hash IN (?)",
				r.db.Model(&model.Image{}).Select("hash").Where("deleted_at IS NULL")).
			Select("site, COALESCE(SUM(upload_count),0) AS count, COUNT(*) AS unique").
			Group("site").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, rw := range rows {
			out.BySite[rw.Site] = SiteStats{Count: rw.Count, Unique: rw.Unique}
		}
	}

	return out, nil
}
