package editspec_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

const chartFixture = `{
  "characters": [{"key": "c1", "name": "寧々", "aliases": []}, {"key": "c2", "name": "めぐる", "aliases": []}],
  "sections": [{"key": "s1", "title": "寧々ルート", "title_zh": null, "characters": ["c1"]}],
  "entries": [{"key": "e1", "label": "START", "unlocked_by": null}],
  "saves": [{"key": "v1", "label": "セーブ1"}],
  "endings": [{"key": "n1", "name": "寧々END", "name_zh": null, "ending_type": "heroine", "characters": ["c1"]}],
  "segments": [
    {"key": "g1", "section": null, "from": {"node_type": "entry", "key": "e1"}, "condition": null, "condition_zh": null,
     "steps": [{"step_type": "choice", "text": "帰る", "text_zh": null, "alt": [], "alt_zh": [], "achievement": false, "save": null},
               {"step_type": "save", "text": null, "text_zh": null, "alt": [], "alt_zh": [], "achievement": false, "save": "v1"}],
     "to": {"node_type": "open", "key": null}},
    {"key": "g2", "section": "s1", "from": {"node_type": "save", "key": "v1"}, "condition": null, "condition_zh": null,
     "steps": [{"step_type": "choice", "text": "手伝う", "text_zh": "帮忙", "alt": [], "alt_zh": [], "achievement": true, "save": null}],
     "to": {"node_type": "ending", "key": "n1"}}
  ]
}`

func chartValue(t *testing.T, edit func(map[string]any)) map[string]any {
	t.Helper()
	var c map[string]any
	if err := json.Unmarshal([]byte(chartFixture), &c); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(c)
	}
	return c
}

func newWalkthroughEngine(t *testing.T) *editing.Engine {
	t.Helper()
	e := newBirthEngine(t)
	if err := testDB.Exec("TRUNCATE catalog_walkthrough_character, catalog_walkthrough RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return e
}

func importedWalkthrough(t *testing.T, workID int64, characterIDs ...int64) *model.CatalogWalkthrough {
	t.Helper()
	raw, _ := json.Marshal(chartValue(t, nil))
	w := &model.CatalogWalkthrough{
		WorkID: workID, SourceID: model.SourceSeiya, SourceURL: "https://seiya-saiga.com/game/x.html",
		ChartVersion: 1, Chart: raw, SourceHash: "h", ExtractModel: "m",
	}
	if err := testDB.Create(w).Error; err != nil {
		t.Fatal(err)
	}
	for i, id := range characterIDs {
		if err := testDB.Create(&model.CatalogWalkthroughCharacter{
			WalkthroughID: w.ID, ChartKey: []string{"c1", "c2"}[i], CharacterID: id, MatchedBy: "rule:walkthrough-same-name",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func character(t *testing.T, name string) int64 {
	t.Helper()
	c := model.CatalogCharacter{DisplayName: name, Extra: []byte(`{}`)}
	if err := testDB.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func trustedEditor() editing.PolicyContext {
	a := realActor(100, "editor")
	a.TrustTier = editing.TrustedTier
	return a
}

func links(t *testing.T, walkthroughID int64) map[string]model.CatalogWalkthroughCharacter {
	t.Helper()
	var rows []model.CatalogWalkthroughCharacter
	if err := testDB.Where("walkthrough_id = ?", walkthroughID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]model.CatalogWalkthroughCharacter{}
	for _, r := range rows {
		out[r.ChartKey] = r
	}
	return out
}

func TestWalkthroughIsBornOnAWorkAndCarriesNoSourcePage(t *testing.T) {
	e := newWalkthroughEngine(t)
	work := createWork(t, "サノバウィッチ")
	prop, rev, err := e.CreateBirth(testCtx, editing.CreateBirthInput{
		EntityType: editspec.TypeWalkthrough,
		Params:     map[string]any{editspec.BirthParamWorkID: float64(work.ID)},
		Patch: map[string]any{
			editspec.FieldWalkthroughChart: chartValue(t, nil),
			editspec.FieldWalkthroughLang:  model.WalkthroughLangZhHans,
		},
		Actor: trustedEditor(),
	})
	if err != nil {
		t.Fatalf("birth: %v", err)
	}
	if rev == nil || prop.Status != editing.StatusMerged {
		t.Fatalf("a trusted editor's walkthrough must apply at once: status %d", prop.Status)
	}
	var w model.CatalogWalkthrough
	if err := testDB.First(&w, rev.EntityID).Error; err != nil {
		t.Fatal(err)
	}
	if w.WorkID != work.ID || w.SourceID != model.SourceNextMoe || !strings.HasPrefix(w.SourceURL, model.WalkthroughNativeURLPrefix) || w.Lang != model.WalkthroughLangZhHans {
		t.Fatalf("born walkthrough = %+v", w)
	}
	if !provenance.IsHuman(provenance.FirstSource(w.FieldProvenance, "chart")) {
		t.Fatalf("the born chart must carry a human stamp: %s", w.FieldProvenance)
	}
	var c map[string]any
	if err := json.Unmarshal(w.Chart, &c); err != nil || len(c["segments"].([]any)) != 2 {
		t.Fatalf("born chart = %s", w.Chart)
	}
}

func TestWalkthroughSnapshotRoundTripsWithoutChanges(t *testing.T) {
	e := newWalkthroughEngine(t)
	work := createWork(t, "w")
	w := importedWalkthrough(t, work.ID, character(t, "綾地寧々"))
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWalkthrough, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap[editspec.FieldWalkthroughHidden] != false || snap[editspec.FieldWalkthroughLang] != model.WalkthroughLangJa {
		t.Fatalf("snapshot = %v", snap)
	}
	raw, _ := json.Marshal(snap)
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	_, _, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWalkthrough, EntityID: w.ID,
		Patch: map[string]any{
			editspec.FieldWalkthroughChart: wire[editspec.FieldWalkthroughChart],
			editspec.FieldWalkthroughPins:  wire[editspec.FieldWalkthroughPins],
		},
		Actor: trustedEditor(),
	})
	if !errors.Is(err, editing.ErrNoEffectiveChanges) {
		t.Fatalf("resubmitting the snapshot: %v, want ErrNoEffectiveChanges", err)
	}
}

func TestWalkthroughChartEditDropsLinksOfRemovedCharactersAndPinsAreCurated(t *testing.T) {
	e := newWalkthroughEngine(t)
	work := createWork(t, "w")
	nene, meguru, real := character(t, "寧々"), character(t, "めぐる"), character(t, "綾地寧々")
	w := importedWalkthrough(t, work.ID, nene, meguru)

	_, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWalkthrough, EntityID: w.ID,
		Patch: map[string]any{
			editspec.FieldWalkthroughChart: chartValue(t, func(c map[string]any) {
				c["characters"] = c["characters"].([]any)[:1]
			}),
			editspec.FieldWalkthroughPins: []any{map[string]any{"chart_key": "c1", "character_id": float64(real)}},
		},
		Actor: trustedEditor(),
	})
	if err != nil || rev == nil {
		t.Fatalf("edit: %v", err)
	}
	got := links(t, w.ID)
	if len(got) != 1 || got["c1"].CharacterID != real || got["c1"].MatchedBy != model.WalkthroughMatchedByCurated {
		t.Fatalf("links = %+v", got)
	}

	_, rev, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWalkthrough, EntityID: w.ID,
		Patch: map[string]any{editspec.FieldWalkthroughHidden: true},
		Actor: trustedEditor(),
	})
	if err != nil || rev == nil {
		t.Fatalf("hide: %v", err)
	}
	var reloaded model.CatalogWalkthrough
	testDB.First(&reloaded, w.ID)
	if reloaded.Status != model.WalkthroughStatusHidden {
		t.Fatalf("status = %d", reloaded.Status)
	}

	other := createWork(t, "the right work")
	_, rev, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWalkthrough, EntityID: w.ID,
		Patch: map[string]any{editspec.FieldWalkthroughWork: float64(other.ID)},
		Actor: trustedEditor(),
	})
	if err != nil || rev == nil {
		t.Fatalf("move: %v", err)
	}
	testDB.First(&reloaded, w.ID)
	if reloaded.WorkID != other.ID || !provenance.IsHuman(provenance.FirstSource(reloaded.FieldProvenance, "work_id")) {
		t.Fatalf("moved walkthrough = work %d, provenance %s", reloaded.WorkID, reloaded.FieldProvenance)
	}
}

func TestWalkthroughEditFromAnUntrustedEditorQueues(t *testing.T) {
	e := newWalkthroughEngine(t)
	work := createWork(t, "w")
	w := importedWalkthrough(t, work.ID)
	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWalkthrough, EntityID: w.ID,
		Patch: map[string]any{editspec.FieldWalkthroughHidden: true},
		Actor: realActor(300, "user"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rev != nil || prop.Status != editing.StatusOpen {
		t.Fatalf("an untrusted edit must queue, got status %d", prop.Status)
	}
}

func TestWalkthroughRefusesBrokenCharts(t *testing.T) {
	e := newWalkthroughEngine(t)
	work := createWork(t, "w")
	w := importedWalkthrough(t, work.ID)
	cases := map[string]map[string]any{
		"unknown key": {editspec.FieldWalkthroughChart: chartValue(t, func(c map[string]any) { c["prose"] = "x" })},
		"dangling ending": {editspec.FieldWalkthroughChart: chartValue(t, func(c map[string]any) {
			c["endings"] = []any{}
		})},
		"duplicate key": {editspec.FieldWalkthroughChart: chartValue(t, func(c map[string]any) {
			chars := c["characters"].([]any)
			chars[1].(map[string]any)["key"] = "c1"
		})},
		"empty choice": {editspec.FieldWalkthroughChart: chartValue(t, func(c map[string]any) {
			seg := c["segments"].([]any)[0].(map[string]any)
			seg["steps"].([]any)[0].(map[string]any)["text"] = " "
		})},
		"pin of a key the chart lacks": {editspec.FieldWalkthroughPins: []any{map[string]any{"chart_key": "c9", "character_id": float64(1)}}},
		"bad lang":                     {editspec.FieldWalkthroughLang: "en"},
		"missing work":                 {editspec.FieldWalkthroughWork: float64(999999)},
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
				EntityType: editspec.TypeWalkthrough, EntityID: w.ID, Patch: patch, Actor: trustedEditor(),
			})
			var valErr *editing.ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("got %v, want a ValidationError", err)
			}
		})
	}
}

func TestUnhidingACompletedWalkthroughTakesItOffTheBacklog(t *testing.T) {
	e := newWalkthroughEngine(t)
	work := createWork(t, "w")
	w := importedWalkthrough(t, work.ID)
	if err := testDB.Model(w).Updates(map[string]any{
		"status": model.WalkthroughStatusHidden, "import_review": `{"missed":["x"],"end_markers":2,"endings_reached":1,"coverage":0.5}`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWalkthrough, EntityID: w.ID,
		Patch:      map[string]any{editspec.FieldWalkthroughHidden: false},
		Actor:      trustedEditor(),
	})
	if err != nil || rev == nil {
		t.Fatalf("unhide: %v", err)
	}
	var reloaded model.CatalogWalkthrough
	testDB.First(&reloaded, w.ID)
	if reloaded.Status != model.WalkthroughStatusLive || len(reloaded.ImportReview) != 0 {
		t.Fatalf("unhidden walkthrough = status %d, import_review %s", reloaded.Status, reloaded.ImportReview)
	}
}
