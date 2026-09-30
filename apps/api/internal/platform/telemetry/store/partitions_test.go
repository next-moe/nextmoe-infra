package store

import (
	"context"
	"testing"
	"time"
)

func TestEnsurePartitions(t *testing.T) {
	now := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	if err := st.EnsurePartitions(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	for i := -1; i <= 3; i++ {
		name := partitionName(today.AddDate(0, 0, i))
		var n int64
		if err := testDB.Raw(`SELECT COUNT(*) FROM pg_class WHERE relname = ?`, name).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("missing partition %s", name)
		}
	}
}

func TestDropExpiredPartitions(t *testing.T) {
	now := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	if err := st.EnsurePartitions(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	old := now.AddDate(0, 0, -31)
	keepEdge := now.AddDate(0, 0, -30)
	if err := ensureDayPartition(testDB, old); err != nil {
		t.Fatal(err)
	}
	if err := ensureDayPartition(testDB, keepEdge); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`CREATE TABLE IF NOT EXISTS telemetry_event_legacy PARTITION OF telemetry_event FOR VALUES FROM ('1990-01-01') TO ('1990-01-02')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.DropExpiredPartitions(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	assertRel := func(name string, want int64) {
		t.Helper()
		var n int64
		if err := testDB.Raw(`SELECT COUNT(*) FROM pg_class WHERE relname = ?`, name).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("%s count=%d want %d", name, n, want)
		}
	}
	assertRel(partitionName(old), 0)
	assertRel(partitionName(keepEdge), 1)
	assertRel("telemetry_event_legacy", 1)
	assertRel(partitionName(now), 1)
	if err := testDB.Exec(`DROP TABLE telemetry_event_legacy`).Error; err != nil {
		t.Fatal(err)
	}
}
