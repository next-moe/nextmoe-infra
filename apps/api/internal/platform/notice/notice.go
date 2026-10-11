package notice

import (
	"time"

	"gorm.io/gorm"
)

type Notice struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Site      string    `gorm:"size:32;not null" json:"site"`
	UserID    int64     `gorm:"not null;index" json:"user_id"`
	Topic     string    `gorm:"size:32;not null" json:"topic"`
	Ref       string    `gorm:"size:64;not null" json:"ref"`
	Title     string    `gorm:"size:120;not null" json:"title"`
	URL       string    `gorm:"size:255;not null;default:''" json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

func (Notice) TableName() string { return "platform_notices" }

type Cursor struct {
	Consumer  string    `gorm:"primaryKey;size:32;column:consumer"`
	LastID    int64     `gorm:"not null;column:last_id"`
	UpdatedAt time.Time `gorm:"not null;column:updated_at"`
}

func (Cursor) TableName() string { return "platform_notice_cursor" }

// A consumer reads id > its cursor. Two raising transactions that overlap can
// commit the larger id first, and a cursor already past it skips the smaller
// one for good; holding this lock until commit makes id order commit order.
const raiseLockKey int64 = 0x6e746963

func RaiseTx(tx *gorm.DB, rows []Notice) error {
	if len(rows) == 0 {
		return nil
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", raiseLockKey).Error; err != nil {
		return err
	}
	return tx.Create(&rows).Error
}

func After(db *gorm.DB, after int64, limit int) ([]Notice, error) {
	var rows []Notice
	err := db.Where("id > ?", after).Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

func Position(db *gorm.DB, consumer string) (int64, error) {
	var cur Cursor
	if err := db.Where("consumer = ?", consumer).Limit(1).Find(&cur).Error; err != nil {
		return 0, err
	}
	return cur.LastID, nil
}

func Advance(db *gorm.DB, consumer string, after int64) error {
	return db.Exec(`
		INSERT INTO platform_notice_cursor (consumer, last_id, updated_at) VALUES (?, ?, now())
		ON CONFLICT (consumer) DO UPDATE SET last_id = EXCLUDED.last_id, updated_at = now()
		 WHERE platform_notice_cursor.last_id < EXCLUDED.last_id`, consumer, after).Error
}
