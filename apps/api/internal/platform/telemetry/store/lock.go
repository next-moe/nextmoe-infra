package store

import (
	"context"
	"log/slog"

	"gorm.io/gorm"
)

const maintenanceLockKey int64 = 0x746c6d74

func (s *Store) TryMaintenance(ctx context.Context, fn func(context.Context) error) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", maintenanceLockKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		if _, uerr := conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", maintenanceLockKey); uerr != nil {
			slog.Error("telemetry maintenance unlock", "err", uerr)
		}
	}()
	return fn(ctx)
}

func (s *Store) DB() *gorm.DB { return s.db }
