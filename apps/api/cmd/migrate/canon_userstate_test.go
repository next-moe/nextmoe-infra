package main

import (
	"database/sql"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserStateHoldsCanonIDsAndStillCascadesForLegacyWorks(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if err := migrate.Run(gdb); err != nil {
			t.Fatal(err)
		}
	}
	if err := seed.Run(gdb); err != nil {
		t.Fatal(err)
	}
	sdb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	var fks int
	_ = sdb.QueryRow(`SELECT count(*) FROM pg_constraint WHERE conname IN ('fk_catalog_user_work_state_work', 'fk_catalog_user_work_view_work')`).Scan(&fks)
	if fks != 0 {
		t.Fatalf("%d user-state FKs to catalog_work survived the migration", fks)
	}
	state := func(work int64) error {
		_, err := sdb.Exec(`INSERT INTO catalog_user_work_state (actor_uid, work_id, state, created_at, updated_at) VALUES (424242, $1, 1, now(), now())
			ON CONFLICT DO NOTHING`, work)
		return err
	}
	if err := state(9_000_000_001); err != nil {
		t.Errorf("a canon work id is refused: %v", err)
	}
	var legacy int64
	if err := sdb.QueryRow(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, display_nsfw, created_at, updated_at)
		VALUES ((SELECT id FROM catalog_medium ORDER BY id LIMIT 1), 'ja', 'cascade', 0, 0, false, now(), now()) RETURNING id`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if err := state(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := sdb.Exec(`DELETE FROM catalog_work WHERE id = $1`, legacy); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = sdb.QueryRow(`SELECT count(*) FROM catalog_user_work_state WHERE work_id = $1`, legacy).Scan(&left)
	if left != 0 {
		t.Errorf("deleting a legacy work left %d state rows", left)
	}
	_, _ = sdb.Exec(`DELETE FROM catalog_user_work_state WHERE actor_uid = 424242`)
}
