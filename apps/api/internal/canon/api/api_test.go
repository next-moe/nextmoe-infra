package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"api/internal/canon/ent"
	"api/internal/canon/migrations"
	"api/internal/testsupport/dbtest"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestGetWork(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn+" search_path=canon")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrations.Up(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	w, err := client.Work.Create().SetMedium("novel").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	handler, _ := New(client)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v3/catalog/works/"+strconv.FormatInt(w.ID, 10), nil))
	var got Work
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.ID != strconv.FormatInt(w.ID, 10) || got.Medium != "novel" {
		t.Fatalf("GET work: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v3/catalog/works/"+strconv.FormatInt(w.ID+1000, 10), nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("GET missing work: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}
