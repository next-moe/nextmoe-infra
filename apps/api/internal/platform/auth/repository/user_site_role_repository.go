package repository

import (
	"context"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserSiteRoleRepository struct {
	db *gorm.DB
}

func NewUserSiteRoleRepository(db *gorm.DB) *UserSiteRoleRepository {
	return &UserSiteRoleRepository{db: db}
}

func (r *UserSiteRoleRepository) ActiveRoleNames(ctx context.Context, userID, siteID uint) ([]string, error) {
	var names []string
	err := r.db.WithContext(ctx).
		Model(&model.UserSiteRole{}).
		Where("user_id = ? AND site_id = ?", userID, siteID).
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Order("role_name ASC").
		Pluck("role_name", &names).Error
	return names, err
}

func (r *UserSiteRoleRepository) ActiveRoleNamesAllSites(ctx context.Context, userID uint) ([]string, error) {
	var names []string
	err := r.db.WithContext(ctx).
		Model(&model.UserSiteRole{}).
		Distinct("role_name").
		Where("user_id = ?", userID).
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Order("role_name ASC").
		Pluck("role_name", &names).Error
	return names, err
}

func (r *UserSiteRoleRepository) ActiveRoleNamesForUsers(ctx context.Context, userIDs []uint, siteID uint) (map[uint][]string, error) {
	out := make(map[uint][]string)
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		UserID   uint
		RoleName string
	}
	err := r.db.WithContext(ctx).
		Model(&model.UserSiteRole{}).
		Select("user_id, role_name").
		Where("user_id IN ? AND site_id = ?", userIDs, siteID).
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Order("user_id ASC, role_name ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.UserID] = append(out[row.UserID], row.RoleName)
	}
	return out, nil
}

func (r *UserSiteRoleRepository) ListByUser(ctx context.Context, userID uint) ([]model.UserSiteRole, error) {
	var rows []model.UserSiteRole
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("site_id ASC, role_name ASC").
		Find(&rows).Error
	return rows, err
}

func (r *UserSiteRoleRepository) Grant(ctx context.Context, g *model.UserSiteRole) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "site_id"}, {Name: "role_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"granted_by", "granted_at", "expires_at", "note"}),
		}).
		Create(g).Error
}

func (r *UserSiteRoleRepository) Revoke(ctx context.Context, userID, siteID uint, roleName string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND site_id = ? AND role_name = ?", userID, siteID, roleName).
		Delete(&model.UserSiteRole{}).Error
}
