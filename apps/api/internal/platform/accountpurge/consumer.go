package accountpurge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	authRepo "api/internal/platform/auth/repository"

	"gorm.io/gorm"
)

const pageSize = 100

type Cursor struct {
	Consumer     string    `gorm:"primaryKey;column:consumer"`
	AnonymizedAt time.Time `gorm:"not null;column:anonymized_at"`
	UserID       int64     `gorm:"not null;column:user_id"`
	UpdatedAt    time.Time `gorm:"not null;column:updated_at"`
}

func (Cursor) TableName() string { return "account_purge_cursor" }

type Feed interface {
	ListDeletedAfter(ctx context.Context, at time.Time, id uint, limit int) ([]authRepo.DeletedUser, error)
}

type Consumer struct {
	Name  string
	Feed  Feed
	DB    *gorm.DB
	Purge func(ctx context.Context, uid int64) error
}

// ExecuteDueDeletions was fixed (#305) to log a failing row and go on, because
// a failed row stays due and is retried. Here a failed account must stop the
// run instead: the cursor is the only record of what is left, and moving it
// past a failure drops that account's purge for good.
func (c *Consumer) Run(ctx context.Context) (int, error) {
	at, id, err := c.position(ctx)
	if err != nil {
		return 0, err
	}
	done := 0
	for {
		rows, err := c.Feed.ListDeletedAfter(ctx, at, id, pageSize)
		if err != nil {
			return done, err
		}
		if len(rows) == 0 {
			return done, nil
		}
		for _, r := range rows {
			if err := c.Purge(ctx, int64(r.ID)); err != nil {
				return done, fmt.Errorf("purge user %d: %w", r.ID, err)
			}
			if err := c.advance(ctx, r.AnonymizedAt, r.ID); err != nil {
				return done, err
			}
			at, id = r.AnonymizedAt, r.ID
			done++
		}
	}
}

func (c *Consumer) position(ctx context.Context) (time.Time, uint, error) {
	var cur Cursor
	err := c.DB.WithContext(ctx).Where("consumer = ?", c.Name).Limit(1).Find(&cur).Error
	if err != nil {
		return time.Time{}, 0, err
	}
	if cur.Consumer == "" {
		return time.Unix(0, 0), 0, nil
	}
	return cur.AnonymizedAt, uint(cur.UserID), nil
}

func (c *Consumer) advance(ctx context.Context, at time.Time, id uint) error {
	return c.DB.WithContext(ctx).Exec(`
		INSERT INTO account_purge_cursor (consumer, anonymized_at, user_id, updated_at)
		VALUES (?, ?, ?, now())
		ON CONFLICT (consumer) DO UPDATE
		   SET anonymized_at = EXCLUDED.anonymized_at, user_id = EXCLUDED.user_id, updated_at = now()
		 WHERE (account_purge_cursor.anonymized_at, account_purge_cursor.user_id)
		     < (EXCLUDED.anonymized_at, EXCLUDED.user_id)`,
		c.Name, at, int64(id)).Error
}

func Start(ctx context.Context, c *Consumer) {
	run := func() {
		n, err := c.Run(ctx)
		if err != nil {
			slog.Error("account purge run failed", "consumer", c.Name, "purged", n, "err", err)
			return
		}
		if n > 0 {
			slog.Info("account purge run", "consumer", c.Name, "purged", n)
		}
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		run()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
