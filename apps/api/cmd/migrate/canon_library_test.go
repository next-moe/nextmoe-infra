package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"api/internal/canon/api"
	"api/internal/canon/commit"
	canonent "api/internal/canon/ent"
	"api/internal/platform/catalog/migrate"
	"api/internal/testsupport/dbtest"
	"api/pkg/utils"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type userToken map[string]uint

func (u userToken) Parse(_ context.Context, token string) (*utils.TokenClaims, error) {
	id, ok := u[token]
	if !ok {
		return nil, errors.New("bad token")
	}
	return &utils.TokenClaims{UserUUID: token, ID: id}, nil
}

func TestLibraryStateProgressAndFeed(t *testing.T) {
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
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": {Raw: raw("novel")}}},
		{Ref: "p", Entity: "work_part", Values: map[string]commit.Value{"work_id": {Raw: raw("ref:w")}, "kind": {Raw: raw("volume")}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	const uid = 880011
	_, _ = cdb.Exec(`DELETE FROM public.catalog_user_work_state WHERE actor_uid = $1`, uid)
	h, _ := api.New(client, api.Options{Verifier: userToken{"reader": uid}})
	do := func(method, path, body string, out any) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer reader")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			_ = json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	w, p := strconv.FormatInt(res.IDs["w"], 10), strconv.FormatInt(res.IDs["p"], 10)

	var st api.WorkState
	if c := do(http.MethodPut, "/v3/me/works/"+w+"/state", `{"state":"wish"}`, &st); c != http.StatusOK || st.State != "wish" {
		t.Fatalf("put state: %d %+v", c, st)
	}
	stale := st.UpdatedAt.Add(-time.Hour).Format(time.RFC3339Nano)
	var conflict map[string]any
	if c := do(http.MethodPut, "/v3/me/works/"+w+"/state", `{"state":"done","observed_at":"`+stale+`"}`, &conflict); c != http.StatusConflict || conflict["current"] == nil {
		t.Errorf("stale write: %d %v", c, conflict)
	}
	if c := do(http.MethodPut, "/v3/me/works/"+w+"/state", `{"state":"done","observed_at":"`+st.UpdatedAt.Format(time.RFC3339Nano)+`"}`, &st); c != http.StatusOK || st.State != "done" {
		t.Errorf("fresh write: %d %+v", c, st)
	}
	if c := do(http.MethodPut, "/v3/me/works/999999999999/state", `{"state":"wish"}`, nil); c != http.StatusNotFound {
		t.Errorf("unknown work: %d", c)
	}
	if c := do(http.MethodPut, "/v3/me/parts/"+p+"/progress", `{"state":"done"}`, nil); c != http.StatusOK {
		t.Errorf("progress: %d", c)
	}
	if c := do(http.MethodDelete, "/v3/me/works/"+w+"/state", "", nil); c/100 != 2 {
		t.Errorf("delete: %d", c)
	}
	var feed api.MeChangeList
	if c := do(http.MethodGet, "/v3/me/changes", "", &feed); c != http.StatusOK {
		t.Fatalf("changes: %d", c)
	}
	var kinds []string
	for _, ch := range feed.Items {
		s := "nil"
		if ch.State != nil {
			s = *ch.State
		}
		kinds = append(kinds, ch.Kind+":"+s)
	}
	got := strings.Join(kinds, ",")
	if !strings.Contains(got, "part_progress:done") || !strings.HasSuffix(got, "work_state:nil") {
		t.Errorf("feed %s", got)
	}
	var next api.MeChangeList
	do(http.MethodGet, "/v3/me/changes?cursor="+feed.NextCursor, "", &next)
	if len(next.Items) != 0 {
		t.Errorf("the cursor replays %d changes", len(next.Items))
	}
}
