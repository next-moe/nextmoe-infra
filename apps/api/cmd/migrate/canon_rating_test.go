package main

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBookRatingsWithDimensions(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	ctx := context.Background()
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	for pass := 1; pass <= 2; pass++ {
		if err := migrate.Run(gdb); err != nil {
			t.Fatalf("legacy migration pass %d: %v", pass, err)
		}
	}
	if err := seed.Run(gdb); err != nil {
		t.Fatalf("legacy seed: %v", err)
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
		{Ref: "n", Entity: "work", Values: map[string]commit.Value{"medium": {Raw: raw("novel")}}},
		{Ref: "m", Entity: "work", Values: map[string]commit.Value{"medium": {Raw: raw("manga")}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var legacy int64
	if err := cdb.QueryRow(`INSERT INTO public.catalog_work (medium_id, olang, display_name, content_rating, status, display_nsfw, created_at, updated_at)
		VALUES ((SELECT id FROM public.catalog_medium ORDER BY id LIMIT 1), 'ja', 'rating', 0, 0, false, now(), now()) RETURNING id`).Scan(&legacy); err != nil {
		t.Fatalf("legacy work: %v", err)
	}
	const uid, other = 880021, 880022
	for _, u := range []int{uid, other} {
		_, _ = cdb.Exec(`DELETE FROM public.catalog_user_work_rating WHERE actor_uid = $1`, u)
	}
	h, _ := api.New(client, api.Options{Verifier: userToken{"reader": uid, "other": other}})
	do := func(token, method, path, body string, out any) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
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
	novel, manga := strconv.FormatInt(res.IDs["n"], 10), strconv.FormatInt(res.IDs["m"], 10)

	var dims map[string][]map[string]string
	if c := do("", http.MethodGet, "/v3/catalog/rating-dimensions", "", &dims); c != http.StatusOK || len(dims["novel"]) == 0 || dims["novel"][2]["key"] != "writing" {
		t.Errorf("dimensions: %d %v", c, dims)
	}
	body := `{"overall":8,"dimensions":{"story":9,"writing":7,"illustration":10},"recommend":"yes","short_summary":"**好看**,插画||剧透||"}`
	var r api.MyRating
	if c := do("reader", http.MethodPut, "/v3/me/works/"+novel+"/rating", body, &r); c != http.StatusOK {
		t.Fatalf("put: %d %+v", c, r)
	}
	if r.Dimensions["writing"] != 7 || r.SpoilerLevel != "none" || r.Recommend != "yes" || !strings.Contains(r.ShortSummaryHTML, "<strong>") {
		t.Errorf("stored rating: %+v", r)
	}
	first := r.UpdatedAt
	time.Sleep(10 * time.Millisecond)
	if c := do("reader", http.MethodPut, "/v3/me/works/"+novel+"/rating", body, &r); c != http.StatusOK || !r.UpdatedAt.Equal(first) {
		t.Errorf("an unchanged PUT moved updated_at: %d %v → %v", c, first, r.UpdatedAt)
	}
	for _, bad := range []struct{ work, body string }{
		{manga, `{"overall":8,"dimensions":{"illustration":9},"recommend":"yes"}`},
		{novel, `{"overall":8,"dimensions":{"story":11},"recommend":"yes"}`},
		{novel, `{"overall":0,"recommend":"yes"}`},
	} {
		if c := do("reader", http.MethodPut, "/v3/me/works/"+bad.work+"/rating", bad.body, nil); c != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", bad.body, c)
		}
	}
	if c := do("reader", http.MethodPut, "/v3/me/works/"+strconv.FormatInt(legacy, 10)+"/rating", `{"overall":8,"recommend":"yes"}`, nil); c != http.StatusNotFound {
		t.Errorf("a legacy-only work through v3: %d", c)
	}
	stale := first.Add(-time.Hour).Format(time.RFC3339Nano)
	var conflict map[string]any
	if c := do("reader", http.MethodPut, "/v3/me/works/"+novel+"/rating", `{"overall":3,"recommend":"no","observed_at":"`+stale+`"}`, &conflict); c != http.StatusConflict || conflict["current"] == nil {
		t.Errorf("stale write: %d %v", c, conflict)
	}
	if c := do("other", http.MethodPut, "/v3/me/works/"+novel+"/rating", `{"overall":6,"dimensions":{"story":5},"recommend":"neutral"}`, nil); c != http.StatusOK {
		t.Fatalf("second reader: %d", c)
	}
	var sum api.RatingSummary
	if c := do("", http.MethodGet, "/v3/catalog/works/"+novel+"/rating-summary", "", &sum); c != http.StatusOK {
		t.Fatalf("summary: %d", c)
	}
	if sum.Count != 2 || sum.Mean != 7 || sum.Distribution[7] != 1 || sum.Recommend["neutral"] != 1 || len(sum.Dimensions) != 3 ||
		sum.Dimensions[0].Key != "story" || sum.Dimensions[0].Mean != 7 || sum.Dimensions[0].Count != 2 {
		t.Errorf("summary %+v", sum)
	}

	if c := do("reader", http.MethodDelete, "/v3/me/works/"+novel+"/rating", "", nil); c/100 != 2 {
		t.Errorf("delete: %d", c)
	}
	if c := do("reader", http.MethodGet, "/v3/me/works/"+novel+"/rating", "", nil); c != http.StatusNotFound {
		t.Errorf("after delete: %d", c)
	}
	var tomb int
	_ = cdb.QueryRow(`SELECT count(*) FROM public.catalog_user_work_rating_tombstone WHERE work_id = $1`, res.IDs["n"]).Scan(&tomb)
	if tomb == 0 {
		t.Error("a v3 delete left no v2 tombstone")
	}
	if c := do("other", http.MethodPut, "/v3/me/works/"+manga+"/rating", `{"overall":9,"dimensions":{"paneling":10},"recommend":"strong_yes"}`, nil); c != http.StatusOK {
		t.Errorf("manga: %d", c)
	}
	var feed api.MeChangeList
	if c := do("other", http.MethodGet, "/v3/me/changes", "", &feed); c != http.StatusOK {
		t.Fatalf("changes: %d", c)
	}
	var seen []string
	for _, ch := range feed.Items {
		if ch.Kind == "rating" && ch.Rating != nil {
			seen = append(seen, ch.SubjectID+":"+strconv.Itoa(ch.Rating.Overall))
		}
	}
	if strings.Join(seen, ",") != novel+":6,"+manga+":9" {
		t.Errorf("rating changes %v", seen)
	}
	var mine api.MeChangeList
	do("reader", http.MethodGet, "/v3/me/changes", "", &mine)
	if n := len(mine.Items); n == 0 || mine.Items[n-1].Kind != "rating" || mine.Items[n-1].Rating != nil {
		t.Errorf("the removal is not the last change: %+v", mine.Items)
	}
}
