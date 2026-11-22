package main

import (
	"context"
	"database/sql"
	"testing"

	"api/internal/canon/xref"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestXrefImportCopiesExactLiveWorkAnchors(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	ctx := context.Background()
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(gdb); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(gdb); err != nil {
		t.Fatal(err)
	}
	if err := migrateCanon(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	sdb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	work := func(deleted bool) int64 {
		var id int64
		del := "NULL"
		if deleted {
			del = "now()"
		}
		if err := sdb.QueryRow(`INSERT INTO public.catalog_work (medium_id, olang, display_name, content_rating, status, display_nsfw, created_at, updated_at, deleted_at)
			VALUES ((SELECT id FROM catalog_medium ORDER BY id LIMIT 1), 'ja', 'xref', 0, 0, false, now(), now(), ` + del + `) RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	live, gone := work(false), work(true)
	ref := func(entity int64, source, ext string, kind int) {
		if _, err := sdb.Exec(`INSERT INTO public.catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by, created_at)
			VALUES (5, $1, (SELECT id FROM catalog_source WHERE key = $2), $3, $4, 'test', now())`, entity, source, ext, kind); err != nil {
			t.Fatalf("ref %s:%s: %v", source, ext, err)
		}
	}
	ref(live, "vndb", "v900001", 0)
	ref(live, "bangumi", "900001", 1)
	ref(gone, "vndb", "v900002", 0)

	if _, err := xref.ImportLegacy(ctx, sdb); err != nil {
		t.Fatal(err)
	}
	if _, err := xref.ImportLegacy(ctx, sdb); err != nil {
		t.Fatalf("a second import: %v", err)
	}
	var got []string
	rows, err := sdb.Query(`SELECT source || ':' || external_id FROM canon.xref_link WHERE external_id IN ('v900001', '900001', 'v900002') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		got = append(got, s)
	}
	rows.Close()
	if len(got) != 1 || got[0] != "vndb:v900001" {
		t.Errorf("imported %v; want only the exact anchor of the live work", got)
	}
}
