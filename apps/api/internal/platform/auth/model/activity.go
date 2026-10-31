package model

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type UserActiveHour struct {
	UserID uint      `gorm:"primaryKey;autoIncrement:false"`
	Hour   time.Time `gorm:"primaryKey;index"`
}

func (UserActiveHour) TableName() string { return "user_active_hours" }

// AddUserActivityColumns adds users.last_login_at and users.last_active_at
// (2026-10-31) and backfills them once from the sessions table, in the same
// step, so that the backfill runs exactly when the columns are created and
// never again.
//
// Until then the platform kept no sign-in time at all: on 2026-10-31 a
// moderation pass had to judge "inactive for a month" from per-site activity
// tables, because sessions are deleted on logout, expiry and ban. What the
// backfill can recover is therefore only what live sessions still hold:
//
//   - last_login_at = the newest session's sign-in time (auth_time for an
//     account-center sign-in, created_at for a site sign-in by code exchange);
//   - last_active_at = the newest of a session's created/last-used/rotated
//     times.
//
// Accounts with no live session stay NULL ("never seen since tracking began").
// user_active_hours is not backfilled: one timestamp per session cannot say
// which hours a user was active, so the activity statistics start at deploy.
func AddUserActivityColumns(db *gorm.DB) error {
	if !db.Migrator().HasTable("users") || db.Migrator().HasColumn(&User{}, "last_login_at") {
		return nil
	}
	stmts := []string{
		`ALTER TABLE users ADD COLUMN last_login_at timestamptz, ADD COLUMN last_active_at timestamptz`,
	}
	if db.Migrator().HasTable("sessions") {
		stmts = append(stmts, `
UPDATE users u SET last_login_at = s.login_at, last_active_at = s.active_at
FROM (
  SELECT user_id,
         max(COALESCE(auth_time, created_at)) AS login_at,
         max(GREATEST(created_at, COALESCE(last_used_at, created_at), COALESCE(rotated_at, created_at))) AS active_at
  FROM sessions GROUP BY user_id
) s
WHERE s.user_id = u.id`)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, s := range stmts {
			if err := tx.Exec(s).Error; err != nil {
				return fmt.Errorf("user activity migrate: %w", err)
			}
		}
		return nil
	})
}
