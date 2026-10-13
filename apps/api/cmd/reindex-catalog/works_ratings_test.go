package main

import (
	"math"
	"testing"

	"api/internal/platform/catalog/model"
)

func TestLoadWorkRatingSortShrinksAgainstTheIndexedPopulation(t *testing.T) {
	truncateFacetTables(t)
	if err := facetTestDB.Exec("TRUNCATE catalog_work_rating RESTART IDENTITY").Error; err != nil {
		t.Fatalf("truncate ratings: %v", err)
	}

	a := mkWork(t, "a", model.WorkStatusLive, galgameMedium)
	b := mkWork(t, "b", model.WorkStatusLive, galgameMedium)
	hltbOnly := mkWork(t, "hltb-only", model.WorkStatusLive, galgameMedium)
	stub := mkWork(t, "stub", model.WorkStatusStub, galgameMedium)
	asmr := mkWork(t, "asmr", model.WorkStatusLive, 5)
	gone := mkWork(t, "gone", model.WorkStatusLive, galgameMedium)
	if err := facetTestDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, gone).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	rate := func(work int64, source string, score float64, votes int) {
		t.Helper()
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_rating (work_id, source_id, score, vote_count, created_at, updated_at)
			SELECT ?, id, ?, ?, now(), now() FROM catalog_source WHERE key = ?`, work, score, votes, source).Error; err != nil {
			t.Fatalf("rate %d on %s: %v", work, source, err)
		}
	}
	rate(a, "bangumi", 9.0, 2)
	rate(a, "vndb", 7.1, 30)
	rate(b, "bangumi", 7.0, 200)
	rate(b, "dlsite", 4.5, 100)
	rate(hltbOnly, "howlongtobeat", 80, 5)
	for _, w := range []int64{stub, asmr, gone} {
		rate(w, "bangumi", 1.0, 1000)
	}

	got, err := loadWorkRatingSort(facetTestDB)
	if err != nil {
		t.Fatal(err)
	}
	c := float64(model.RatingPriorVotes)
	bangumiMean := (9.0 + 7.0) / 2
	want := map[int64]map[string]float64{
		a: {"bangumi": (c*bangumiMean + 2*9.0) / (c + 2), "vndb": 7.1},
		b: {"bangumi": (c*bangumiMean + 200*7.0) / (c + 200), "dlsite": 4.5},
	}
	if len(got) != len(want) {
		t.Fatalf("rated works = %v, want only %d and %d (no hltb, stub, asmr or deleted work)", got, a, b)
	}
	for work, sources := range want {
		if len(got[work]) != len(sources) {
			t.Fatalf("work %d = %v, want %v", work, got[work], sources)
		}
		for src, v := range sources {
			if math.Abs(got[work][src]-v) > 1e-9 {
				t.Fatalf("work %d %s = %v, want %v", work, src, got[work][src], v)
			}
		}
	}
}
