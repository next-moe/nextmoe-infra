package repository

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/image/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type ImageRepository struct {
	db *gorm.DB
}

func NewImageRepository(db *gorm.DB) *ImageRepository {
	return &ImageRepository{db: db}
}

func (r *ImageRepository) FindByHash(ctx context.Context, hash string) (*model.Image, error) {
	var img model.Image
	err := r.db.WithContext(ctx).
		Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).
		Where("hash = ? AND deleted_at IS NULL", hash).
		First(&img).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &img, nil
}

func (r *ImageRepository) Create(ctx context.Context, img *model.Image) (bool, error) {
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "hash"}}, DoNothing: true}).
		Create(img)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *ImageRepository) FindByHashIncludingDeleted(ctx context.Context, hash string) (*model.Image, error) {
	var img model.Image
	err := r.db.WithContext(ctx).
		Session(&gorm.Session{Logger: r.db.Logger.LogMode(logger.Silent)}).
		Where("hash = ?", hash).
		First(&img).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &img, nil
}

func (r *ImageRepository) Resurrect(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).
		Model(&model.Image{}).
		Where("hash = ?", hash).
		Updates(map[string]any{"deleted_at": nil, "last_referenced_at": time.Now()}).
		Error
}

func (r *ImageRepository) UpdateVariants(ctx context.Context, hash string, variants []string) error {
	img := &model.Image{}
	img.SetVariants(variants)
	return r.db.WithContext(ctx).
		Model(&model.Image{}).
		Where("hash = ?", hash).
		Update("variants", img.Variants).
		Error
}

// referenceRefreshAfter skips rows renewed recently: GC only acts after 60
// days cold, while the daily reference-ping rewrote all ~900k rows (a
// non-HOT update, last_referenced_at is indexed) every day.
const referenceRefreshAfter = 7 * 24 * time.Hour

func (r *ImageRepository) TouchReferenced(ctx context.Context, hashes []string) (int64, error) {
	if len(hashes) == 0 {
		return 0, nil
	}
	now := time.Now()
	res := r.db.WithContext(ctx).
		Model(&model.Image{}).
		Where("hash IN ? AND deleted_at IS NULL AND last_referenced_at < ?", hashes, now.Add(-referenceRefreshAfter)).
		Update("last_referenced_at", now)
	return res.RowsAffected, res.Error
}

type ImageMeta struct {
	Hash      string `json:"-"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Thumbhash string `json:"thumbhash,omitempty"`
	Sexual    *int16 `json:"sexual,omitempty"`
}

type imageMetaRow struct {
	Hash       string `gorm:"column:hash"`
	Width      int    `gorm:"column:width"`
	Height     int    `gorm:"column:height"`
	Thumbhash  string `gorm:"column:thumbhash"`
	GradeLevel *int   `gorm:"column:grade_level"`
}

// The machine ladder tops out at an explicit sexual act (3), which the public
// scale does not distinguish from nudity — both are "explicit", the strongest
// value the wire carries. Same fold as internal/jobs/imagegradesync.
func publicSexual(level int) (int16, bool) {
	switch level {
	case 0, 1, 2:
		return int16(level), true
	case 3:
		return 2, true
	default:
		return 0, false
	}
}

func (r *ImageRepository) MetaByHashes(ctx context.Context, hashes []string) (map[string]ImageMeta, error) {
	out := make(map[string]ImageMeta, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}
	var rows []imageMetaRow
	if err := r.db.WithContext(ctx).
		Model(&model.Image{}).
		Select("hash", "width", "height", "thumbhash",
			"(review_labels->'grade'->>'level')::int AS grade_level").
		Where("hash IN ? AND deleted_at IS NULL", hashes).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		m := ImageMeta{Hash: row.Hash, Width: row.Width, Height: row.Height, Thumbhash: row.Thumbhash}
		if row.GradeLevel != nil {
			if sexual, ok := publicSexual(*row.GradeLevel); ok {
				m.Sexual = &sexual
			}
		}
		out[m.Hash] = m
	}
	return out, nil
}
