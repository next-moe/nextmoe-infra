package service

import (
	"context"
	stderrors "errors"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTagBlockActorRequired  = stderrors.New("catalog: a tag block requires a reporting user")
	ErrTagBlockTagUnavailable = stderrors.New("catalog: tag not available to block")
	ErrTagBlockLimit          = stderrors.New("catalog: blocked-tag cap reached")
)

type UserTagBlockService struct{ db *gorm.DB }

func NewUserTagBlockService(db *gorm.DB) *UserTagBlockService {
	return &UserTagBlockService{db: db}
}

type TagBlockRecord struct {
	ID        int64
	TagID     int64
	Mode      int16
	CreatedAt time.Time
	UpdatedAt time.Time
}

func tagBlockRecord(row model.CatalogUserTagBlock) TagBlockRecord {
	return TagBlockRecord{ID: row.ID, TagID: row.TagID, Mode: row.Mode, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s *UserTagBlockService) Block(ctx context.Context, uid, tagID int64, mode int16, clientID, site string) (*TagBlockRecord, error) {
	if uid <= 0 {
		return nil, ErrTagBlockActorRequired
	}
	var out TagBlockRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.CatalogUserTagBlock
		err := tx.Where("actor_uid = ? AND tag_id = ?", uid, tagID).Take(&existing).Error
		if err == nil {
			if existing.Mode != mode {
				now := time.Now()
				if err := tx.Model(&model.CatalogUserTagBlock{}).Where("id = ?", existing.ID).
					Updates(map[string]any{"mode": mode, "updated_at": now}).Error; err != nil {
					return err
				}
				existing.Mode, existing.UpdatedAt = mode, now
			}
			out = tagBlockRecord(existing)
			return nil
		}
		if !stderrors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var n int64
		if err := tx.Model(&model.CatalogTag{}).Where("id = ?", tagID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrTagBlockTagUnavailable
		}
		var count int64
		if err := tx.Model(&model.CatalogUserTagBlock{}).Where("actor_uid = ?", uid).Count(&count).Error; err != nil {
			return err
		}
		if count >= model.TagBlocksPerUserMax {
			return ErrTagBlockLimit
		}
		row := model.CatalogUserTagBlock{ActorUID: uid, TagID: tagID, Mode: mode, ClientID: clientID, Site: site}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "actor_uid"}, {Name: "tag_id"}},
			DoNothing: true,
		}).Create(&row).Error; err != nil {
			return err
		}
		var stored model.CatalogUserTagBlock
		if err := tx.Where("actor_uid = ? AND tag_id = ?", uid, tagID).Take(&stored).Error; err != nil {
			return err
		}
		out = tagBlockRecord(stored)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *UserTagBlockService) Unblock(ctx context.Context, uid, tagID int64) error {
	if uid <= 0 {
		return ErrTagBlockActorRequired
	}
	return s.db.WithContext(ctx).Where("actor_uid = ? AND tag_id = ?", uid, tagID).
		Delete(&model.CatalogUserTagBlock{}).Error
}

func (s *UserTagBlockService) ListMineFor(ctx context.Context, uid int64, tagIDs []int64) ([]TagBlockRecord, error) {
	if uid <= 0 {
		return nil, ErrTagBlockActorRequired
	}
	if len(tagIDs) == 0 {
		return nil, nil
	}
	var rows []model.CatalogUserTagBlock
	if err := s.db.WithContext(ctx).Where("actor_uid = ? AND tag_id IN ?", uid, tagIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TagBlockRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, tagBlockRecord(r))
	}
	return out, nil
}

func (s *UserTagBlockService) ListMine(ctx context.Context, uid int64, beforeID int64, limit int) ([]TagBlockRecord, error) {
	if uid <= 0 {
		return nil, ErrTagBlockActorRequired
	}
	q := s.db.WithContext(ctx).Model(&model.CatalogUserTagBlock{}).Where("actor_uid = ?", uid)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.CatalogUserTagBlock
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TagBlockRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, tagBlockRecord(r))
	}
	return out, nil
}

func (s *UserTagBlockService) CountMine(ctx context.Context, uid int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrTagBlockActorRequired
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserTagBlock{}).Where("actor_uid = ?", uid).Count(&n).Error
	return n, err
}
