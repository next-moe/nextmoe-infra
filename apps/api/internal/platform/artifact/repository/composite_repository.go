package repository

import (
	"context"

	"api/internal/platform/artifact/model"

	"gorm.io/gorm"
)

type CompositeRepository struct {
	db *gorm.DB
}

func NewCompositeRepository(db *gorm.DB) *CompositeRepository {
	return &CompositeRepository{db: db}
}

func (r *CompositeRepository) LiveArtifacts(ctx context.Context, uuids []string) ([]model.Artifact, error) {
	var items []model.Artifact
	err := r.db.WithContext(ctx).Where("uuid IN ?", uuids).Find(&items).Error
	return items, err
}

func (r *CompositeRepository) Find(ctx context.Context, publicID, site string) (*model.Composite, error) {
	var c model.Composite
	err := r.db.WithContext(ctx).
		Where("public_id = ? AND site_key = ?", publicID, site).
		First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CompositeRepository) Create(ctx context.Context, c *model.Composite, publish func() error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		return publish()
	})
}

func (r *CompositeRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Composite{}, id).Error
}

func (r *CompositeRepository) ListBySite(ctx context.Context, site string, offset, limit int) ([]model.Composite, int64, error) {
	var (
		items []model.Composite
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Composite{}).Where("site_key = ?", site)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *CompositeRepository) Referencing(ctx context.Context, artifactID uint) ([]model.Composite, error) {
	var items []model.Composite
	err := r.db.WithContext(ctx).
		Where("id IN (SELECT composite_id FROM composite_sources WHERE artifact_id = ?)", artifactID).
		Find(&items).Error
	return items, err
}

func (r *CompositeRepository) PinnedUUIDs(ctx context.Context, compositeIDs []uint) (map[uint][]string, error) {
	var rows []struct {
		CompositeID uint
		UUID        string
	}
	err := r.db.WithContext(ctx).
		Table("composite_sources AS cs").
		Select("cs.composite_id, a.uuid").
		Joins("JOIN artifacts a ON a.id = cs.artifact_id").
		Where("cs.composite_id IN ?", compositeIDs).
		Order("cs.composite_id, cs.position").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint][]string, len(compositeIDs))
	for _, row := range rows {
		out[row.CompositeID] = append(out[row.CompositeID], row.UUID)
	}
	return out, nil
}
