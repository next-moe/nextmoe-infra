package repository

import (
	"context"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
)

type EditorApplicationRepository struct {
	db *gorm.DB
}

func NewEditorApplicationRepository(db *gorm.DB) *EditorApplicationRepository {
	return &EditorApplicationRepository{db: db}
}

func (r *EditorApplicationRepository) Create(ctx context.Context, app *model.EditorApplication) error {
	return r.db.WithContext(ctx).Create(app).Error
}

func (r *EditorApplicationRepository) FindLatestByUser(ctx context.Context, userID uint) (*model.EditorApplication, error) {
	var app model.EditorApplication
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).Order("id DESC").First(&app).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *EditorApplicationRepository) FindByID(ctx context.Context, id uint) (*model.EditorApplication, error) {
	var app model.EditorApplication
	if err := r.db.WithContext(ctx).First(&app, id).Error; err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *EditorApplicationRepository) ListByStatus(ctx context.Context, status string, page, limit int) ([]model.EditorApplication, int64, error) {
	var items []model.EditorApplication
	var total int64
	q := r.db.WithContext(ctx).Model(&model.EditorApplication{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error
	return items, total, err
}

func reviewPending(tx *gorm.DB, id uint, status string, reviewerID uint, reason string) (int64, error) {
	res := tx.Model(&model.EditorApplication{}).
		Where("id = ? AND status = ?", id, model.EditorAppPending).
		Updates(map[string]any{
			"status":         status,
			"reviewer_id":    reviewerID,
			"reviewed_at":    time.Now(),
			"decline_reason": reason,
		})
	return res.RowsAffected, res.Error
}

func (r *EditorApplicationRepository) Approve(ctx context.Context, id, userID, reviewerID uint, roleName string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if n, err = reviewPending(tx, id, model.EditorAppApproved, reviewerID, ""); err != nil || n == 0 {
			return err
		}
		return tx.Exec(`INSERT INTO user_roles (user_id, role_id)
			SELECT ?, id FROM roles WHERE name = ?
			ON CONFLICT DO NOTHING`, userID, roleName).Error
	})
	return n, err
}

func (r *EditorApplicationRepository) Decline(ctx context.Context, id, reviewerID uint, reason string) (int64, error) {
	return reviewPending(r.db.WithContext(ctx), id, model.EditorAppDeclined, reviewerID, reason)
}
