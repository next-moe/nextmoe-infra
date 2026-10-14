package repository

import (
	"context"
	"time"

	"api/internal/platform/artifact/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ArtifactRepository struct {
	db *gorm.DB
}

func NewArtifactRepository(db *gorm.DB) *ArtifactRepository {
	return &ArtifactRepository{db: db}
}

func (r *ArtifactRepository) Create(ctx context.Context, a *model.Artifact) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *ArtifactRepository) FindByUUID(ctx context.Context, uuid string) (*model.Artifact, error) {
	var a model.Artifact
	if err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *ArtifactRepository) Update(ctx context.Context, a *model.Artifact) error {
	return r.db.WithContext(ctx).Save(a).Error
}

func (r *ArtifactRepository) ListBySite(ctx context.Context, site string, offset, limit int) ([]model.Artifact, int64, error) {
	var (
		items []model.Artifact
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Artifact{}).Where("site_key = ?", site)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *ArtifactRepository) SaveManifest(ctx context.Context, m *model.Manifest) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "artifact_id"}},
		UpdateAll: true,
	}).Create(m).Error
}

func (r *ArtifactRepository) Touch(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).
		Model(&model.Artifact{}).
		Where("id = ?", id).
		Update("updated_at", time.Now()).Error
}

func (r *ArtifactRepository) FindOrphans(ctx context.Context, before time.Time, limit int) ([]model.Artifact, error) {
	var items []model.Artifact
	err := r.db.WithContext(ctx).
		Where("status = ? AND updated_at < ?", model.StatusUploading, before).
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *ArtifactRepository) FindExpiredSoftDeleted(ctx context.Context, before time.Time, limit int) ([]model.Artifact, error) {
	var items []model.Artifact
	err := r.db.WithContext(ctx).Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", before).
		Where("NOT EXISTS (SELECT 1 FROM composite_sources cs WHERE cs.artifact_id = artifacts.id)").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *ArtifactRepository) HardDelete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Unscoped().Delete(&model.Artifact{}, id).Error
}
