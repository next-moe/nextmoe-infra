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
	ErrFollowActorRequired      = stderrors.New("catalog: a follow requires a reporting user")
	ErrFollowCompanyUnavailable = stderrors.New("catalog: company not available to follow")
	ErrFollowLimit              = stderrors.New("catalog: followed-company cap reached")
)

type UserEntityFollowService struct{ db *gorm.DB }

func NewUserEntityFollowService(db *gorm.DB) *UserEntityFollowService {
	return &UserEntityFollowService{db: db}
}

type EntityFollowRecord struct {
	ID        int64
	EntityID  int64
	CreatedAt time.Time
}

func followRecord(row model.CatalogUserEntityFollow) EntityFollowRecord {
	return EntityFollowRecord{ID: row.ID, EntityID: row.EntityID, CreatedAt: row.CreatedAt}
}

func (s *UserEntityFollowService) FollowCompany(ctx context.Context, uid, companyID int64, clientID, site string) (*EntityFollowRecord, error) {
	if uid <= 0 {
		return nil, ErrFollowActorRequired
	}
	var out EntityFollowRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.CatalogUserEntityFollow
		err := tx.Where("actor_uid = ? AND entity_type = ? AND entity_id = ?",
			uid, model.EntityTypeLabel, companyID).Take(&existing).Error
		if err == nil {
			out = followRecord(existing)
			return nil
		}
		if !stderrors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var n int64
		if err := tx.Model(&model.CatalogLabel{}).Where("id = ?", companyID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrFollowCompanyUnavailable
		}
		var count int64
		if err := tx.Model(&model.CatalogUserEntityFollow{}).
			Where("actor_uid = ? AND entity_type = ?", uid, model.EntityTypeLabel).
			Count(&count).Error; err != nil {
			return err
		}
		if count >= model.EntityFollowsPerUserMax {
			return ErrFollowLimit
		}
		row := model.CatalogUserEntityFollow{
			ActorUID: uid, EntityType: model.EntityTypeLabel, EntityID: companyID,
			ClientID: clientID, Site: site,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "actor_uid"},
				{Name: "entity_type"},
				{Name: "entity_id"},
			},
			DoNothing: true,
		}).Create(&row).Error; err != nil {
			return err
		}
		var stored model.CatalogUserEntityFollow
		if err := tx.Where("actor_uid = ? AND entity_type = ? AND entity_id = ?",
			uid, model.EntityTypeLabel, companyID).Take(&stored).Error; err != nil {
			return err
		}
		out = followRecord(stored)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *UserEntityFollowService) UnfollowCompany(ctx context.Context, uid, companyID int64) error {
	if uid <= 0 {
		return ErrFollowActorRequired
	}
	return s.db.WithContext(ctx).
		Where("actor_uid = ? AND entity_type = ? AND entity_id = ?", uid, model.EntityTypeLabel, companyID).
		Delete(&model.CatalogUserEntityFollow{}).Error
}

func (s *UserEntityFollowService) ListMineFor(ctx context.Context, uid int64, companyIDs []int64) ([]EntityFollowRecord, error) {
	if uid <= 0 {
		return nil, ErrFollowActorRequired
	}
	if len(companyIDs) == 0 {
		return nil, nil
	}
	var rows []model.CatalogUserEntityFollow
	if err := s.db.WithContext(ctx).
		Where("actor_uid = ? AND entity_type = ? AND entity_id IN ?", uid, model.EntityTypeLabel, companyIDs).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]EntityFollowRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, followRecord(r))
	}
	return out, nil
}

func (s *UserEntityFollowService) ListMine(ctx context.Context, uid int64, beforeID int64, limit int) ([]EntityFollowRecord, error) {
	if uid <= 0 {
		return nil, ErrFollowActorRequired
	}
	q := s.db.WithContext(ctx).Model(&model.CatalogUserEntityFollow{}).
		Where("actor_uid = ? AND entity_type = ?", uid, model.EntityTypeLabel)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.CatalogUserEntityFollow
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]EntityFollowRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, followRecord(r))
	}
	return out, nil
}

func (s *UserEntityFollowService) CountMine(ctx context.Context, uid int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrFollowActorRequired
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserEntityFollow{}).
		Where("actor_uid = ? AND entity_type = ?", uid, model.EntityTypeLabel).
		Count(&n).Error
	return n, err
}

func (s *UserEntityFollowService) CountFollowers(ctx context.Context, companyID int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserEntityFollow{}).
		Where("entity_type = ? AND entity_id = ?", model.EntityTypeLabel, companyID).
		Count(&n).Error
	return n, err
}
