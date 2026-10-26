package docsync

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func titlesOf(t *testing.T, workID int64) [][4]any {
	t.Helper()
	all, err := loadWorkTitles(facetTestDB, nil)
	if err != nil {
		t.Fatalf("loadWorkTitles: %v", err)
	}
	out := make([][4]any, 0, len(all[workID]))
	for _, r := range all[workID] {
		out = append(out, [4]any{r.lang, r.title, r.latin, r.kind})
	}
	return out
}

func TestLoadWorkTitlesUsesCatalogRowsForEveryWork(t *testing.T) {
	truncateFacetTables(t)

	claimed := mkWork(t, "claimed-title", model.WorkStatusLive, galgameMedium)
	if err := facetTestDB.Exec(
		`UPDATE catalog_work SET site = 'kungal', product_work_id = 97001 WHERE id = ?`,
		claimed).Error; err != nil {
		t.Fatalf("claim: %v", err)
	}
	for _, r := range []struct {
		lang, title, latin string
		kind               int16
	}{
		{"ja", "日本語名", "Nihongo Mei", model.WorkTitleKindOfficial},
		{"en", "English Name", "", model.WorkTitleKindOfficial},
		{"", "べつめい", "", model.WorkTitleKindAlias},
		{"ja", "日名", "", model.WorkTitleKindAbbreviation},
		{"ja", "ディーエルサイト名", "", model.WorkTitleKindSearchHint},
	} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, latin, kind, provenance)
			VALUES (?, ?, ?, nullif(?, ''), ?, 0)`, claimed, r.lang, r.title, r.latin, r.kind).Error; err != nil {
			t.Fatalf("seed native title: %v", err)
		}
	}

	bodyless := mkWork(t, "bodyless-title", model.WorkStatusLive, galgameMedium)
	for _, r := range []struct {
		lang, title, latin string
		kind               int16
	}{
		{"ja", "無体作品", "Mutai Sakuhin", model.WorkTitleKindOfficial},
		{"", "むたい", "", model.WorkTitleKindAlias},
		{"ja", "けんさくヒント", "", model.WorkTitleKindSearchHint},
	} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, latin, kind, provenance)
			VALUES (?, ?, ?, nullif(?, ''), ?, 0)`, bodyless, r.lang, r.title, r.latin, r.kind).Error; err != nil {
			t.Fatalf("seed native title: %v", err)
		}
	}

	stub := mkWork(t, "stub-title", model.WorkStatusStub, galgameMedium)
	if err := facetTestDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
		VALUES (?, 'ja', 'スタブ', 0, 0)`, stub).Error; err != nil {
		t.Fatalf("seed stub title: %v", err)
	}

	got := titlesOf(t, claimed)
	want := [][4]any{
		{"en", "English Name", "", model.WorkTitleKindOfficial},
		{"ja", "日本語名", "Nihongo Mei", model.WorkTitleKindOfficial},
		{"", "べつめい", "", model.WorkTitleKindAlias},
		{"ja", "日名", "", model.WorkTitleKindAbbreviation},
		{"ja", "ディーエルサイト名", "", model.WorkTitleKindSearchHint},
	}
	if len(got) != len(want) {
		t.Fatalf("claimed titles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claimed titles[%d] = %v, want %v (whole set %v)", i, got[i], want[i], got)
		}
	}

	got = titlesOf(t, bodyless)
	if len(got) != 3 {
		t.Fatalf("bodyless titles = %v, want all three native rows verbatim", got)
	}
	if got[0] != ([4]any{"ja", "無体作品", "Mutai Sakuhin", model.WorkTitleKindOfficial}) {
		t.Fatalf("bodyless titles[0] = %v", got[0])
	}
	if got[2] != ([4]any{"ja", "けんさくヒント", "", model.WorkTitleKindSearchHint}) {
		t.Fatalf("bodyless keeps its search hint: %v", got)
	}

	if got = titlesOf(t, stub); len(got) != 0 {
		t.Fatalf("a non-LIVE work contributed titles: %v", got)
	}
}

func TestLoadWorkTitlesPutsTheDisplayNameFirstInItsLanguage(t *testing.T) {
	truncateFacetTables(t)
	w := mkWork(t, "魔女っ娘ア・ラ・モードII", model.WorkStatusLive, galgameMedium)
	for _, title := range []string{"魔女っ娘ア・ラ・モード2", "魔女っ娘ア・ラ・モードII"} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
			VALUES (?, 'ja', ?, 0, 0)`, w, title).Error; err != nil {
			t.Fatalf("seed title: %v", err)
		}
	}
	got := titlesOf(t, w)
	if len(got) != 2 || got[0][1] != "魔女っ娘ア・ラ・モードII" {
		t.Fatalf("titles = %v, want the display name ahead of the older merged-in duplicate", got)
	}
}
