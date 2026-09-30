package store

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"api/internal/platform/telemetry/otlp"

	"gorm.io/gorm"
)

var partitionNameRe = regexp.MustCompile(`^telemetry_event_p[0-9]{8}$`)

func partitionName(day time.Time) string {
	return "telemetry_event_p" + otlp.DateUTC(day).Format("20060102")
}

func ensureDayPartition(db *gorm.DB, day time.Time) error {
	d := otlp.DateUTC(day)
	name := partitionName(d)
	from := d.Format("2006-01-02")
	to := d.AddDate(0, 0, 1).Format("2006-01-02")
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF telemetry_event FOR VALUES FROM ('%s') TO ('%s')`,
		name, from, to,
	)
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("create partition %s: %w", name, err)
	}
	return nil
}

func EnsurePartitions(db *gorm.DB, now time.Time) error {
	today := otlp.DateUTC(now)
	for i := -1; i <= 3; i++ {
		if err := ensureDayPartition(db, today.AddDate(0, 0, i)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) EnsurePartitions(ctx context.Context, now time.Time) error {
	return EnsurePartitions(s.db.WithContext(ctx), now)
}

func DropExpiredPartitions(db *gorm.DB, now time.Time) error {
	cutoff := otlp.DateUTC(now).AddDate(0, 0, -30)
	var names []string
	if err := db.Raw(`
		SELECT c.relname
		  FROM pg_inherits i
		  JOIN pg_class c ON c.oid = i.inhrelid
		  JOIN pg_class p ON p.oid = i.inhparent
		 WHERE p.relname = 'telemetry_event'`).Scan(&names).Error; err != nil {
		return fmt.Errorf("list partitions: %w", err)
	}
	for _, name := range names {
		if !partitionNameRe.MatchString(name) {
			continue
		}
		day, err := time.Parse("20060102", name[len("telemetry_event_p"):])
		if err != nil {
			continue
		}
		day = otlp.DateUTC(day)
		if !day.Before(cutoff) {
			continue
		}
		if err := db.Exec(`DROP TABLE IF EXISTS ` + name).Error; err != nil {
			return fmt.Errorf("drop partition %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) DropExpiredPartitions(ctx context.Context, now time.Time) error {
	return DropExpiredPartitions(s.db.WithContext(ctx), now)
}
