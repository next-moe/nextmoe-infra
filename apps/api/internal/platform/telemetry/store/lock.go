package store

import (
	"context"
	"log/slog"

	"gorm.io/gorm"
)

const maintenanceLockKey int64 = 0x746c6d74
const engineFetchLockKey int64 = 0x746c6566
const alertDispatchLockKey int64 = 0x746c616c

func (s *Store) TryMaintenance(ctx context.Context, fn func(context.Context) error) error {
	return s.tryLock(ctx, maintenanceLockKey, fn)
}

func (s *Store) TryEngineFetch(ctx context.Context, fn func(context.Context) error) error {
	return s.tryLock(ctx, engineFetchLockKey, fn)
}

func (s *Store) TryAlertDispatch(ctx context.Context, fn func(context.Context) error) error {
	return s.tryLock(ctx, alertDispatchLockKey, fn)
}

func (s *Store) tryLock(ctx context.Context, key int64, fn func(context.Context) error) error {
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
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		if _, uerr := conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key); uerr != nil {
			slog.Error("telemetry lock unlock", "err", uerr, "key", key)
		}
	}()
	return fn(ctx)
}

func (s *Store) DB() *gorm.DB { return s.db }
