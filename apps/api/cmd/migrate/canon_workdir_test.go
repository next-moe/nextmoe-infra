package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"api/internal/canon/commit"
	canonent "api/internal/canon/ent"
	"api/internal/canon/takedown"
	catalogmigrate "api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
	"api/internal/testsupport/dbtest"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A canon work can be kept in a folder, marked and browsed through v2; play
// time and v2 ratings stay with the legacy table they are keyed to.
func TestCanonWorksAreInTheUserLists(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	ctx := context.Background()
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalogmigrate.Run(gdb); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(gdb); err != nil {
		t.Fatal(err)
	}
	if err := migrateCanon(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	if err := catalogmigrate.Run(gdb); err != nil {
		t.Fatalf("catalog migrate after canon: %v", err)
	}
	cdb, err := sql.Open("pgx", dsn+" search_path=canon")
	if err != nil {
		t.Fatal(err)
	}
	defer cdb.Close()
	client := canonent.NewClient(canonent.Driver(entsql.OpenDB(dialect.Postgres, cdb)))
	raw := func(v any) commit.Value { b, _ := json.Marshal(v); return commit.Value{Raw: b} }
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": raw("novel"), "content_rating": raw("r15")}},
		{Entity: "work_title", Values: map[string]commit.Value{"work_id": raw("ref:w"), "text": raw("目録の小説"), "lang": raw("ja"), "kind": raw("official")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	work := res.IDs["w"]
	const uid = int64(77_001)
	var dir struct {
		MediumID, ContentRating, Status int16
		DisplayName                     string
	}
	if err := gdb.Table("catalog_work_directory").Where("id = ?", work).Take(&dir).Error; err != nil || dir.MediumID != 3 || dir.ContentRating != 1 || dir.Status != 0 || dir.DisplayName != "目録の小説" {
		t.Fatalf("directory row %+v %v", dir, err)
	}

	folders := service.NewUserFolderService(gdb)
	f, err := folders.Create(ctx, service.FolderCreate{OwnerUID: uid, Name: "本棚", Visibility: model.FolderVisibilityPublic})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := folders.PutItem(ctx, uid, f.ID, work); err != nil {
		t.Fatalf("add a canon work: %v", err)
	}
	recent, err := folders.FolderRecentWorks(ctx, []int64{f.ID})
	if err != nil || len(recent[f.ID]) != 1 || recent[f.ID][0] != work {
		t.Errorf("recent works %v %v", recent, err)
	}
	if ratings, err := folders.FolderContentRatings(ctx, []int64{f.ID}); err != nil || ratings[f.ID] != 1 {
		t.Errorf("folder rating %v %v", ratings, err)
	}
	if _, err := service.NewUserWorkStateService(gdb).Set(ctx, uid, work, model.WorkStateDoing, nil); err != nil {
		t.Errorf("mark a canon work: %v", err)
	}
	if _, err := service.NewUserRatingService(gdb).Set(ctx, uid, work, "c", "", service.RatingInput{Overall: 8, Recommend: model.RecommendYes}); err == nil {
		t.Error("v2 rated a canon work")
	}

	td, err := takedown.Take(ctx, client, takedown.Notice{NoticeID: "n", Scope: "entity", Entity: "work", EntityID: work, Reason: "r", By: "user:1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.Table("catalog_work_directory").Where("id = ?", work).Take(&dir).Error; err != nil || dir.Status != model.WorkStatusQuarantine {
		t.Errorf("a taken-down work: %+v %v", dir, err)
	}
	if err := takedown.Restore(ctx, client, td.ID, "user:1", "x"); err != nil {
		t.Fatal(err)
	}
}
