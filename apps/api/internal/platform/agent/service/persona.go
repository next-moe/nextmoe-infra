package service

import (
	"context"
	"errors"

	"api/internal/platform/agent/model"

	"gorm.io/gorm"
)

func (s *Store) Personas(ctx context.Context, siteID *int64) ([]model.Persona, error) {
	var rows []model.Persona
	q := s.db.WithContext(ctx).Order("site_id, active DESC, id")
	if siteID != nil {
		q = q.Where("site_id = ?", *siteID)
	}
	return rows, q.Find(&rows).Error
}

func (s *Store) PersonaByID(ctx context.Context, id int64) (*model.Persona, error) {
	var p model.Persona
	err := s.db.WithContext(ctx).Take(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) SavePersona(ctx context.Context, p *model.Persona) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(p).Error; err != nil {
			return err
		}
		if !p.Active {
			return nil
		}
		return tx.Model(&model.Persona{}).Where("site_id = ? AND kind = ? AND id <> ? AND active", p.SiteID, p.Kind, p.ID).
			Update("active", false).Error
	})
}

func (s *Store) DeletePersona(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Delete(&model.Persona{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
