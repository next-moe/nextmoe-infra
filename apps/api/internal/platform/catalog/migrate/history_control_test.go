package migrate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHistoryControlBackfillCoversAccountsAlreadyOff(t *testing.T) {
	const off, on, stamped = int64(91001), int64(91002), int64(91003)
	cleanup := func() {
		testDB.Exec(`DELETE FROM catalog_user_history_control WHERE actor_uid IN (?, ?, ?)`, off, on, stamped)
		testDB.Exec(`DELETE FROM catalog_user_privacy WHERE actor_uid IN (?, ?, ?)`, off, on, stamped)
	}
	cleanup()
	t.Cleanup(cleanup)
	switchedAt := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_user_privacy (actor_uid, play_records_private, browsing_history_off, updated_at)
		VALUES (?, false, true, ?), (?, false, false, ?), (?, false, true, ?)`,
		off, switchedAt, on, switchedAt, stamped, switchedAt).Error)
	later := switchedAt.Add(24 * time.Hour)
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_user_history_control (actor_uid, cleared_at, changed_at) VALUES (?, ?, ?)`,
		stamped, later, later).Error)

	for range 2 {
		require.NoError(t, backfillHistoryControl(testDB))
	}

	var rows []struct {
		ActorUID  int64
		ClearedAt time.Time
		ChangedAt time.Time
	}
	require.NoError(t, testDB.Raw(`SELECT actor_uid, cleared_at, changed_at FROM catalog_user_history_control
		WHERE actor_uid IN (?, ?, ?) ORDER BY actor_uid`, off, on, stamped).Scan(&rows).Error)
	require.Len(t, rows, 2, "an account that never switched off gets no row")
	require.Equal(t, off, rows[0].ActorUID)
	require.True(t, rows[0].ChangedAt.Equal(switchedAt))
	require.True(t, rows[0].ClearedAt.Equal(switchedAt))
	require.Equal(t, stamped, rows[1].ActorUID)
	require.True(t, rows[1].ChangedAt.Equal(later), "an existing row is left alone")
}
