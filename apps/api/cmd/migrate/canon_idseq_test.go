package main

import (
	"context"
	"database/sql"
	"testing"

	canonent "api/internal/canon/ent"
	"api/internal/canon/idseq"
	"api/internal/platform/catalog/migrate"
	catalogModel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCanonOwnsTheLegacyIDSequencesAcrossLegacyMigrations(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	ctx := context.Background()
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("cannot connect to the assigned test database")
	}
	sdb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()

	if err := migrate.Run(gdb); err != nil {
		t.Fatalf("legacy migration: %v", err)
	}
	if err := seed.Run(gdb); err != nil {
		t.Fatalf("legacy seed: %v", err)
	}
	newEngine := func(name string) int64 {
		e := catalogModel.CatalogEngine{Name: name, Aliases: datatypes.JSON(`[]`)}
		if err := gdb.Create(&e).Error; err != nil {
			t.Fatalf("legacy create %s: %v", name, err)
		}
		return e.ID
	}
	before := newEngine("idseq-before")

	for pass := 1; pass <= 2; pass++ {
		if err := idseq.Adopt(ctx, sdb, idseq.Families); err != nil {
			t.Fatalf("adopt pass %d: %v", pass, err)
		}
	}
	for pass := 1; pass <= 2; pass++ {
		if err := migrate.Run(gdb); err != nil {
			t.Fatalf("legacy migration after adopt, pass %d: %v", pass, err)
		}
	}

	for _, f := range idseq.Families {
		var identity string
		if err := sdb.QueryRow(`SELECT is_identity FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = 'id'`, f.Legacy).Scan(&identity); err != nil {
			t.Fatalf("%s: %v", f.Legacy, err)
		}
		draws, err := idseq.DrawsFrom(ctx, sdb, f)
		if err != nil {
			t.Fatalf("%s: %v", f.Legacy, err)
		}
		if identity != "NO" || !draws {
			t.Errorf("%s after legacy AutoMigrate: is_identity=%s, draws from %s: %v", f.Legacy, identity, idseq.QualifiedSequence(f), draws)
		}
	}

	legacy := newEngine("idseq-after")
	if legacy != before+1 {
		t.Errorf("legacy insert after adopt got id %d, want %d", legacy, before+1)
	}
	var canon int64
	if err := sdb.QueryRow(`SELECT nextval('canon_ids.engine')`).Scan(&canon); err != nil {
		t.Fatal(err)
	}
	if canon != legacy+1 {
		t.Errorf("canon draw got %d, want %d", canon, legacy+1)
	}

	for pass := 1; pass <= 2; pass++ {
		if err := migrateCanon(ctx, dsn); err != nil {
			t.Fatalf("canon migration pass %d: %v", pass, err)
		}
	}
	cdb, err := sql.Open("pgx", dsn+" search_path=canon")
	if err != nil {
		t.Fatal(err)
	}
	defer cdb.Close()
	client := canonent.NewClient(canonent.Driver(entsql.OpenDB(dialect.Postgres, cdb)))
	var legacyWork int64
	if err := sdb.QueryRow(`INSERT INTO public.catalog_work (medium_id, olang, display_name, content_rating, status, display_nsfw, created_at, updated_at)
		VALUES ((SELECT id FROM catalog_medium ORDER BY id LIMIT 1), 'ja', 'idseq', 0, 0, false, now(), now()) RETURNING id`).Scan(&legacyWork); err != nil {
		t.Fatalf("legacy work insert: %v", err)
	}
	w, err := client.Work.Create().SetMedium("novel").Save(ctx)
	if err != nil {
		t.Fatalf("canon work create: %v", err)
	}
	if w.ID != legacyWork+1 {
		t.Errorf("canon work got id %d, want %d after legacy %d", w.ID, legacyWork+1, legacyWork)
	}

	if _, err := sdb.Exec(`DROP TABLE public.catalog_engine CASCADE`); err != nil {
		t.Fatal(err)
	}
	var survives bool
	if err := sdb.QueryRow(`SELECT to_regclass('canon_ids.engine') IS NOT NULL`).Scan(&survives); err != nil || !survives {
		t.Errorf("dropping the legacy table took canon_ids.engine with it (err %v)", err)
	}

	if err := migrate.Run(gdb); err != nil {
		t.Fatalf("legacy migration recreating catalog_engine: %v", err)
	}
	if err := idseq.Adopt(ctx, sdb, idseq.Families); err != nil {
		t.Fatalf("adopt over a recreated identity table: %v", err)
	}
	if again := newEngine("idseq-recreated"); again <= canon {
		t.Errorf("recreated legacy table drew id %d, at or below canon's earlier %d", again, canon)
	}
}
