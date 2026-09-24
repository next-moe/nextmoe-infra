package repository

import (
	"context"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
)

func (r *UserRepository) ScheduleDeletion(ctx context.Context, userID uint, requestedAt, dueAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ? AND anonymized_at IS NULL", userID).
		Updates(map[string]any{"deletion_requested_at": requestedAt, "deletion_due_at": dueAt}).Error
}

func (r *UserRepository) CancelDeletion(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).
		Updates(map[string]any{"deletion_requested_at": nil, "deletion_due_at": nil}).Error
}

func (r *UserRepository) FindDueForDeletion(ctx context.Context, now time.Time, limit int) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).Preload("Roles").
		Where("deletion_due_at <= ? AND anonymized_at IS NULL", now).
		Order("deletion_due_at, id").Limit(limit).Find(&users).Error
	return users, err
}

func (r *UserRepository) EraseAccount(ctx context.Context, userID uint, dueBy, at time.Time) (bool, error) {
	erased := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`
			UPDATE users
			   SET name = '已注销#' || id, email = 'deleted-' || id || '@anonymized.invalid',
			       password = NULL, kungal_password = NULL, moyu_password = NULL,
			       avatar = '', avatar_image_hash = NULL, bio = '', ip = '',
			       original_email = NULL, adult_confirmed_at = NULL,
			       status = 1, anonymized_at = ?, deletion_due_at = NULL, updated_at = ?
			 WHERE id = ? AND anonymized_at IS NULL AND deletion_due_at <= ?`, at, at, userID, dueBy)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		erased = true
		for _, st := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM oauth_accounts WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM user_preferences WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM user_site_data WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM user_follows WHERE follower_id = ? OR following_id = ?`, []any{userID, userID}},
			{`DELETE FROM user_roles WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM user_site_roles WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM authorization_codes WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM password_resets WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM sessions WHERE user_id = ?`, []any{userID}},
			{`DELETE FROM user_migrations WHERE user_id = ?`, []any{userID}},
		} {
			if err := tx.Exec(st.sql, st.args...).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return erased, err
}

type DeletedUser struct {
	ID           uint      `gorm:"column:id"`
	UUID         string    `gorm:"column:uuid"`
	AnonymizedAt time.Time `gorm:"column:anonymized_at"`
}

func (r *UserRepository) ListDeletedAfter(ctx context.Context, at time.Time, id uint, limit int) ([]DeletedUser, error) {
	var out []DeletedUser
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, uuid, anonymized_at FROM users
		 WHERE anonymized_at IS NOT NULL AND (anonymized_at, id) > (?, ?)
		   AND anonymized_at < now() - interval '2 minutes'
		 ORDER BY anonymized_at, id LIMIT ?`, at, id, limit).Scan(&out).Error
	return out, err
}
