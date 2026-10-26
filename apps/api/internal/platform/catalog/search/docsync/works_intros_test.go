package docsync

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func introsOf(t *testing.T, workID int64) map[string]string {
	t.Helper()
	all, err := loadWorkIntros(facetTestDB, nil)
	if err != nil {
		t.Fatalf("loadWorkIntros: %v", err)
	}
	out := map[string]string{}
	for _, in := range all[workID] {
		out[in.lang] = in.text
	}
	return out
}

func TestLoadWorkIntrosUsesCatalogRowsForEveryWork(t *testing.T) {
	truncateFacetTables(t)

	claimed := mkWork(t, "claimed-intro", model.WorkStatusLive, galgameMedium)
	if err := facetTestDB.Exec(
		`UPDATE catalog_work SET site = 'kungal', product_work_id = 96001 WHERE id = ?`,
		claimed).Error; err != nil {
		t.Fatalf("claim: %v", err)
	}
	for _, r := range []struct {
		lang, intro string
		source      int16
		provenance  int16
	}{
		{"ja", "日本語のあらすじ", 3, 0},
		{"en", "English synopsis", 3, 0},
		{"zh-Hans", "机翻译文", 2, 1},
		{"zh-Hans", "源文", 3, 0},
	} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_intro (work_id, lang, intro, source_id, provenance)
			VALUES (?, ?, ?, ?, ?)`, claimed, r.lang, r.intro, r.source, r.provenance).Error; err != nil {
			t.Fatalf("seed claimed intro: %v", err)
		}
	}

	bodyless := mkWork(t, "bodyless-intro", model.WorkStatusLive, galgameMedium)
	for _, r := range []struct {
		lang, intro string
		source      int16
		provenance  int16
	}{
		{"zh-Hans", "机翻译文", 2, 1},
		{"zh-Hans", "源文", 3, 0},
		{"ja", "native ja", 2, 0},
	} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_intro (work_id, lang, intro, source_id, provenance)
			VALUES (?, ?, ?, ?, ?)`, bodyless, r.lang, r.intro, r.source, r.provenance).Error; err != nil {
			t.Fatalf("seed native intro: %v", err)
		}
	}

	stub := mkWork(t, "stub-intro", model.WorkStatusStub, galgameMedium)
	if err := facetTestDB.Create(&model.CatalogWorkIntro{
		WorkID: stub, Lang: "ja", Intro: "スタブ", SourceID: 2,
	}).Error; err != nil {
		t.Fatalf("seed stub intro: %v", err)
	}

	got := introsOf(t, claimed)
	want := map[string]string{
		"ja": "日本語のあらすじ", "en": "English synopsis", "zh-Hans": "源文",
	}
	if len(got) != len(want) {
		t.Fatalf("claimed intros = %v, want %v", got, want)
	}
	for lang, text := range want {
		if got[lang] != text {
			t.Fatalf("claimed[%s] = %q, want %q", lang, got[lang], text)
		}
	}
	got = introsOf(t, bodyless)
	if got["zh-Hans"] != "源文" {
		t.Fatalf("bodyless zh-Hans = %q, want the SOURCE row (a machine translation must lose)", got["zh-Hans"])
	}
	if got["ja"] != "native ja" {
		t.Fatalf("bodyless ja = %q", got["ja"])
	}
	if len(got) != 2 {
		t.Fatalf("bodyless intros = %v, want one row per language", got)
	}

	if got = introsOf(t, stub); len(got) != 0 {
		t.Fatalf("a non-LIVE work contributed intros: %v", got)
	}
}

// The search index folds one row per language with its own copy of the ordering,
// so the human lane has to be spelled out here too. It was not, and for one wave
// the detail face rendered the curated ja intro while the index still held
// bangumi's — one field, two consumers, two answers (6 live works).
func TestLoadWorkIntrosGivesTheSearchIndexTheHumanLane(t *testing.T) {
	truncateFacetTables(t)

	const (
		bangumiSource = 3
		curatedSource = 12
	)

	contested := mkWork(t, "contested-intro", model.WorkStatusLive, galgameMedium)
	for _, r := range []struct {
		lang, intro string
		source      int16
		provenance  int16
	}{
		{"ja", "上流の紹介", bangumiSource, 0},
		{"ja", "人手の紹介", curatedSource, 0},
	} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_intro (work_id, lang, intro, source_id, provenance)
			VALUES (?, ?, ?, ?, ?)`, contested, r.lang, r.intro, r.source, r.provenance).Error; err != nil {
			t.Fatalf("seed contested intro: %v", err)
		}
	}
	if got := introsOf(t, contested)["ja"]; got != "人手の紹介" {
		t.Fatalf("indexed ja intro = %q, want the curated row — the index must agree with the detail face", got)
	}

	// And no further: inside the machine tier the human lane decides nothing,
	// or 5,806 works would be indexed under a translation of our `en` row while
	// their ja face keeps rendering bangumi's Japanese original.
	machine := mkWork(t, "machine-tier-intro", model.WorkStatusLive, galgameMedium)
	for _, r := range []struct {
		lang, intro string
		source      int16
		provenance  int16
	}{
		{"zh-Hans", "上流の機械翻訳", bangumiSource, 1},
		{"zh-Hans", "curated 車道の機械翻訳", curatedSource, 1},
	} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_intro (work_id, lang, intro, source_id, provenance)
			VALUES (?, ?, ?, ?, ?)`, machine, r.lang, r.intro, r.source, r.provenance).Error; err != nil {
			t.Fatalf("seed machine-tier intro: %v", err)
		}
	}
	if got := introsOf(t, machine)["zh-Hans"]; got != "上流の機械翻訳" {
		t.Fatalf("indexed zh-Hans intro = %q, want the upstream machine row", got)
	}
}
