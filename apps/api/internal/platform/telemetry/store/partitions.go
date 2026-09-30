package store

import (
	"context"
	"time"

	"api/internal/platform/telemetry/partition"
)

func (s *Store) EnsurePartitions(ctx context.Context, now time.Time) error {
	return partition.EnsurePartitions(s.db.WithContext(ctx), now)
}

func (s *Store) DropExpiredPartitions(ctx context.Context, now time.Time) error {
	return partition.DropExpiredPartitions(s.db.WithContext(ctx), now)
}
