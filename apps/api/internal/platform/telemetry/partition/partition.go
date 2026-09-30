package partition

import (
	"fmt"
	"regexp"
	"time"

	"gorm.io/gorm"
)

var nameRe = regexp.MustCompile(`^telemetry_event_p[0-9]{8}$`)

func dateUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func Name(day time.Time) string {
	return "telemetry_event_p" + dateUTC(day).Format("20060102")
}

func EnsureDay(db *gorm.DB, day time.Time) error {
	d := dateUTC(day)
	name := Name(d)
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
	today := dateUTC(now)
	for i := -30; i <= 3; i++ {
		if err := EnsureDay(db, today.AddDate(0, 0, i)); err != nil {
			return err
		}
	}
	return nil
}

func DropExpiredPartitions(db *gorm.DB, now time.Time) error {
	cutoff := dateUTC(now).AddDate(0, 0, -30)
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
		if !nameRe.MatchString(name) {
			continue
		}
		day, err := time.Parse("20060102", name[len("telemetry_event_p"):])
		if err != nil {
			continue
		}
		day = dateUTC(day)
		if !day.Before(cutoff) {
			continue
		}
		if err := db.Exec(`DROP TABLE IF EXISTS ` + name).Error; err != nil {
			return fmt.Errorf("drop partition %s: %w", name, err)
		}
	}
	return nil
}
