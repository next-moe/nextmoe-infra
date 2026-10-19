package editspec_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func title(lang, name string, kind int16) map[string]any {
	return map[string]any{"lang": lang, "title": name, "kind": float64(kind)}
}

func titleLatin(lang, name, latin string, kind int16) map[string]any {
	el := title(lang, name, kind)
	el["latin"] = latin
	return el
}

func letmoeActor(uid int64, tier int16) editing.PolicyContext {
	return editing.PolicyContext{UserID: uid, Site: "letmoe", TrustTier: tier}
}

var nextProductID int64 = 4242

func createClaimedWork(t *testing.T, site string, displayName, olang string) *model.CatalogWork {
	t.Helper()
	nextProductID++
	productID := nextProductID
	w := &model.CatalogWork{
		MediumID: 1, Site: &site, ProductWorkID: &productID,
		OLang: olang, DisplayName: displayName,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	if err := testDB.Create(w).Error; err != nil {
		t.Fatalf("create claimed work: %v", err)
	}
	return w
}

func loadTitleRows(t *testing.T, workID int64) []model.CatalogWorkTitle {
	t.Helper()
	var rows []model.CatalogWorkTitle
	if err := testDB.Where("work_id = ?", workID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestTitlesValidator(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "validator target")
	editor := realActor(100)

	bad := []struct {
		name  string
		value any
	}{
		{"not an array", "x"},
		{"empty array", []any{}},
		{"non-object element", []any{"x"}},
		{"unknown key", []any{map[string]any{"lang": "ja", "title": "a", "kind": float64(0), "extra": 1}}},
		{"missing lang", []any{map[string]any{"title": "a", "kind": float64(0)}}},
		{"bad lang", []any{title("xx", "a", 0)}},
		{"empty title", []any{title("ja", "  ", 0)}},
		{"long title", []any{title("ja", strings.Repeat("あ", 501), 0)}},
		{"kind out of range", []any{title("ja", "a", 4)}},
		{"kind not integer", []any{map[string]any{"lang": "ja", "title": "a", "kind": 1.5}}},
		{"empty latin", []any{titleLatin("ja", "a", " ", 0)}},
		{"duplicate element", []any{title("ja", "a", 0), title("ja", "a", 0)}},
		{"no official title", []any{title("ja", "a", 1)}},
	}
	var valErr *editing.ValidationError
	for _, c := range bad {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkTitles: c.value}, Actor: editor,
		}); !errors.As(err, &valErr) {
			t.Errorf("%s: want ValidationError, got %v", c.name, err)
		}
	}
}

func TestTitlesApplyAndDerivation(t *testing.T) {
	e := newEngine(t)
	reviewer := realActor(200, "ren")

	mergeTitles := func(t *testing.T, workID int64, titles []any, extra map[string]any) *editing.Revision {
		t.Helper()
		patch := map[string]any{editspec.FieldWorkTitles: titles}
		for k, v := range extra {
			patch[k] = v
		}
		prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: workID, Patch: patch, Actor: realActor(100),
		})
		if err != nil {
			t.Fatalf("propose titles: %v", err)
		}
		rev, err := e.MergeProposal(testCtx, prop.ID, reviewer, "")
		if err != nil {
			t.Fatalf("merge titles: %v", err)
		}
		return rev
	}

	t.Run("full replace + olang official wins", func(t *testing.T) {
		work := createWork(t, "旧名")
		if err := testDB.Create(&model.CatalogWorkTitle{
			WorkID: work.ID, Lang: "en", Title: "stale", Kind: model.WorkTitleKindAlias,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := testDB.Create(&model.CatalogWorkTitle{
			WorkID: work.ID, Lang: "ja", Title: "にほんごめい", Kind: model.WorkTitleKindSearchHint,
		}).Error; err != nil {
			t.Fatal(err)
		}
		mergeTitles(t, work.ID, []any{
			title("zh", "中文名", 0),
			titleLatin("ja", "日本語名", "nihongo mei", 0),
		}, nil)

		rows := loadTitleRows(t, work.ID)
		if len(rows) != 3 {
			t.Fatalf("rows after replace: %+v", rows)
		}
		if rows[0].Kind != model.WorkTitleKindSearchHint ||
			rows[1].Title != "中文名" || rows[2].Title != "日本語名" {
			t.Fatalf("row order/content: %+v", rows)
		}
		if rows[2].Latin == nil || *rows[2].Latin != "nihongo mei" {
			t.Fatalf("latin: %+v", rows[2])
		}
		var w model.CatalogWork
		if err := testDB.First(&w, work.ID).Error; err != nil {
			t.Fatal(err)
		}
		if w.DisplayName != "日本語名" {
			t.Fatalf("derived display_name: %q", w.DisplayName)
		}
	})

	t.Run("no olang official falls back to first official", func(t *testing.T) {
		work := createWork(t, "旧名")
		mergeTitles(t, work.ID, []any{
			title("zh", "中文名", 0),
			title("en", "English Name", 0),
		}, nil)
		var w model.CatalogWork
		if err := testDB.First(&w, work.ID).Error; err != nil {
			t.Fatal(err)
		}
		if w.DisplayName != "中文名" {
			t.Fatalf("fallback display_name: %q", w.DisplayName)
		}
	})

	t.Run("same-merge olang change feeds the derivation", func(t *testing.T) {
		work := createWork(t, "旧名")
		mergeTitles(t, work.ID, []any{
			title("ja", "日本語名", 0),
			title("en", "English Name", 0),
		}, map[string]any{editspec.FieldWorkOLang: "en"})
		var w model.CatalogWork
		if err := testDB.First(&w, work.ID).Error; err != nil {
			t.Fatal(err)
		}
		if w.OLang != "en" || w.DisplayName != "English Name" {
			t.Fatalf("after olang+titles merge: olang=%q display=%q", w.OLang, w.DisplayName)
		}
	})

	t.Run("titles derivation overrides a same-merge display_name", func(t *testing.T) {
		work := createWork(t, "旧名")
		mergeTitles(t, work.ID, []any{title("ja", "タイトル", 0)},
			map[string]any{editspec.FieldWorkDisplayName: "手書き名"})
		var w model.CatalogWork
		if err := testDB.First(&w, work.ID).Error; err != nil {
			t.Fatal(err)
		}
		if w.DisplayName != "タイトル" {
			t.Fatalf("derivation must win: %q", w.DisplayName)
		}
	})

	t.Run("identical titles are a no-op", func(t *testing.T) {
		work := createWork(t, "旧名")
		value := []any{titleLatin("ja", "同一", "douitsu", 0), title("zh", "同一中文", 1)}
		mergeTitles(t, work.ID, value, nil)
		prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkTitles: value}, Actor: realActor(100),
		})
		if err != nil {
			t.Fatalf("no-op propose: %v", err)
		}
		if _, err := e.MergeProposal(testCtx, prop.ID, reviewer, ""); !errors.Is(err, editing.ErrNoEffectiveChanges) {
			t.Fatalf("no-op merge: %v", err)
		}
	})

	t.Run("revert restores titles and the derived display_name", func(t *testing.T) {
		work := createWork(t, "旧名")
		mergeTitles(t, work.ID, []any{title("ja", "初版", 0)}, nil)
		mergeTitles(t, work.ID, []any{title("ja", "第二版", 0), title("zh", "第二版中文", 1)}, nil)

		if _, _, err := e.Revert(testCtx, editing.RevertInput{
			EntityType: editspec.TypeWork, EntityID: work.ID, ToSeq: 1, Actor: reviewer,
		}); err != nil {
			t.Fatalf("revert: %v", err)
		}
		rows := loadTitleRows(t, work.ID)
		if len(rows) != 1 || rows[0].Title != "初版" {
			t.Fatalf("reverted rows: %+v", rows)
		}
		var w model.CatalogWork
		if err := testDB.First(&w, work.ID).Error; err != nil {
			t.Fatal(err)
		}
		if w.DisplayName != "初版" {
			t.Fatalf("reverted display_name: %q", w.DisplayName)
		}
	})
}

// What changed for letmoe when its overlay went: anyone may propose, the
// trusted tier lands its edits on any work instead of only letmoe's claims, and
// a claim no longer lands an untrusted edit because it is letmoe's.
func TestLetmoeUnderThePlatformPolicy(t *testing.T) {
	e := newEngine(t)

	t.Run("trusted tier direct-edits its own claim in one revision", func(t *testing.T) {
		work := createClaimedWork(t, "letmoe", "自家作品", "ja")
		prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkTitles: []any{
				title("ja", "自家作品・改", 0),
			}},
			Actor: letmoeActor(500, editing.TrustedTier),
		})
		if err != nil {
			t.Fatalf("letmoe direct edit: %v", err)
		}
		if rev == nil || rev.Action != editing.ActionDirect || prop.Status != editing.StatusMerged {
			t.Fatalf("must automerge: rev=%+v status=%d", rev, prop.Status)
		}
		revs, err := e.ListRevisions(testCtx, editspec.TypeWork, work.ID, 0)
		if err != nil || len(revs) != 1 {
			t.Fatalf("single revision: %d err=%v", len(revs), err)
		}
		var w model.CatalogWork
		if err := testDB.First(&w, work.ID).Error; err != nil {
			t.Fatal(err)
		}
		if w.DisplayName != "自家作品・改" {
			t.Fatalf("derived display_name: %q", w.DisplayName)
		}
	})

	t.Run("trusted tier now direct-edits works letmoe does not claim", func(t *testing.T) {
		for _, work := range []*model.CatalogWork{
			createWork(t, "公共作品"),
			createClaimedWork(t, "moyu", "他站作品", "ja"),
		} {
			_, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
				EntityType: editspec.TypeWork, EntityID: work.ID,
				Patch: map[string]any{editspec.FieldWorkDisplayName: work.DisplayName + "・改"},
				Actor: letmoeActor(500, editing.TrustedTier),
			})
			if err != nil || rev == nil {
				t.Fatalf("work %d: trusted edit must land: rev=%v err=%v", work.ID, rev, err)
			}
		}
	})

	t.Run("below trusted tier proposes and queues, even on letmoe's own claim", func(t *testing.T) {
		work := createClaimedWork(t, "letmoe", "自家作品", "ja")
		prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkDisplayName: "一般提案"},
			Actor: letmoeActor(501, 0),
		})
		if err != nil {
			t.Fatalf("tier 0 propose: %v", err)
		}
		if rev != nil || prop.Status != editing.StatusOpen {
			t.Fatalf("must stay open: rev=%v status=%d", rev, prop.Status)
		}
		if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "ok"); err != nil {
			t.Fatalf("reviewer merge: %v", err)
		}
	})

	t.Run("the answer is the same on every site", func(t *testing.T) {
		work := createClaimedWork(t, "letmoe", "自家作品", "ja")
		for _, site := range []string{"nextmoe", "kungal", "editor"} {
			_, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
				EntityType: editspec.TypeWork, EntityID: work.ID,
				Patch: map[string]any{editspec.FieldWorkDisplayName: "via " + site},
				Actor: editing.PolicyContext{UserID: 502, Site: site, TrustTier: editing.TrustedTier},
			})
			if err != nil || rev == nil {
				t.Fatalf("%s: trusted edit must land: rev=%v err=%v", site, rev, err)
			}
		}
	})
}

func TestAliasMayHaveNoLanguage(t *testing.T) {
	e := newEngine(t)
	reviewer := realActor(200, "ren")
	work := createWork(t, "alias lang target")

	value := []any{
		title("ja", "日本語名", 0),
		title("", "no-language alias", 1),
	}
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkTitles: value}, Actor: realActor(100),
	})
	if err != nil {
		t.Fatalf("propose a lang-less alias: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, reviewer, ""); err != nil {
		t.Fatalf("merge: %v", err)
	}

	rows := loadTitleRows(t, work.ID)
	if len(rows) != 2 || rows[1].Lang != "" || rows[1].Kind != model.WorkTitleKindAlias {
		t.Fatalf("stored rows: %+v", rows)
	}

	snapshot, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatalf("current snapshot: %v", err)
	}
	encoded, err := json.Marshal(snapshot[editspec.FieldWorkTitles])
	if err != nil {
		t.Fatal(err)
	}
	var wire any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkTitles: wire},
		Actor: realActor(100),
	}); err != nil && !errors.Is(err, editing.ErrNoEffectiveChanges) {
		t.Fatalf("the field must accept its own snapshot: %v", err)
	}
}

func TestOnlyAliasMayHaveNoLanguage(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "alias lang negative")
	var valErr *editing.ValidationError
	for _, c := range []struct {
		name  string
		value any
	}{
		{"official with no lang", []any{title("", "a", 0)}},
		{"abbreviation with no lang", []any{title("ja", "a", 0), title("", "b", 2)}},
	} {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkTitles: c.value}, Actor: realActor(100),
		}); !errors.As(err, &valErr) {
			t.Errorf("%s: want ValidationError, got %v", c.name, err)
		}
	}
}
