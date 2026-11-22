package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"api/internal/canon/commit"
	canonent "api/internal/canon/ent"
	"api/internal/canon/merge"
	"api/internal/platform/catalog/migrate"
	"api/internal/testsupport/dbtest"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCanonWorkMergeFoldsUserState(t *testing.T) {
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
	if err := migrateCanon(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	cdb, err := sql.Open("pgx", dsn+" search_path=canon")
	if err != nil {
		t.Fatal(err)
	}
	defer cdb.Close()
	client := canonent.NewClient(canonent.Driver(entsql.OpenDB(dialect.Postgres, cdb)))
	s := commit.New(client)
	mk := func() int64 {
		b, _ := json.Marshal("novel")
		res, err := s.Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"},
			Changes: []commit.Change{{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": {Raw: b}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return res.IDs["w"]
	}
	survivor, loser := mk(), mk()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := cdb.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	const uid = 777001
	exec(`DELETE FROM public.catalog_user_work_state WHERE actor_uid = $1`, uid)
	exec(`INSERT INTO public.catalog_user_work_state (actor_uid, work_id, state, created_at, updated_at) VALUES ($1, $2, 1, now(), now() - interval '1 day')`, uid, survivor)
	exec(`INSERT INTO public.catalog_user_work_state (actor_uid, work_id, state, created_at, updated_at) VALUES ($1, $2, 3, now(), now())`, uid, loser)
	exec(`INSERT INTO public.catalog_user_work_state (actor_uid, work_id, state, created_at, updated_at) VALUES ($1, $2, 2, now(), now())`, uid+1, loser)
	var folder int64
	if err := cdb.QueryRow(`INSERT INTO public.catalog_user_folder (owner_uid, name, description, visibility, is_default, item_count, created_at, updated_at, follower_count)
		VALUES ($1, 'f', '', 0, false, 1, now(), now(), 0) RETURNING id`, uid).Scan(&folder); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO public.catalog_user_folder_item (folder_id, work_id, owner_uid, created_at, updated_at, position, note, is_cover) VALUES ($1, $2, $3, now(), now(), 0, 'n', false)`, folder, loser, uid)

	id, err := merge.Merge(ctx, client, "work", loser, survivor, "user:1", "duplicate")
	if err != nil {
		t.Fatal(err)
	}
	var state, moved, item int
	_ = cdb.QueryRow(`SELECT state FROM public.catalog_user_work_state WHERE actor_uid = $1 AND work_id = $2`, uid, survivor).Scan(&state)
	_ = cdb.QueryRow(`SELECT count(*) FROM public.catalog_user_work_state WHERE actor_uid = $1 AND work_id = $2`, uid+1, survivor).Scan(&moved)
	_ = cdb.QueryRow(`SELECT count(*) FROM public.catalog_user_folder_item WHERE folder_id = $1 AND work_id = $2`, folder, survivor).Scan(&item)
	if state != 3 || moved != 1 || item != 1 {
		t.Errorf("after merge: later state %d (want 3), moved %d, folder item %d", state, moved, item)
	}

	if err := merge.Unmerge(ctx, client, id, "user:1"); err != nil {
		t.Fatal(err)
	}
	var back, itemBack int
	_ = cdb.QueryRow(`SELECT count(*) FROM public.catalog_user_work_state WHERE work_id = $1`, loser).Scan(&back)
	_ = cdb.QueryRow(`SELECT count(*) FROM public.catalog_user_folder_item WHERE folder_id = $1 AND work_id = $2`, folder, loser).Scan(&itemBack)
	if back != 2 || itemBack != 1 {
		t.Errorf("after unmerge: loser state rows %d (want 2), folder item %d", back, itemBack)
	}
	exec(`DELETE FROM public.catalog_user_work_state WHERE actor_uid IN ($1, $2)`, uid, uid+1)
	exec(`DELETE FROM public.catalog_user_folder_item WHERE folder_id = $1`, folder)
	exec(`DELETE FROM public.catalog_user_folder WHERE id = $1`, folder)
}
