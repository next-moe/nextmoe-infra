package editspec_test

import (
	"errors"
	"fmt"
	"sort"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/textnorm"
)

// importAlias reproduces cmd/import-work-aliases' bgm lane verbatim: the row is
// inserted from upstream truth with a fresh identity column, which is why the
// suppression key may never be derived from it.
func importAlias(t *testing.T, workID int64, lang, title string, kind int16) int64 {
	t.Helper()
	if err := testDB.Exec(
		`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
		 VALUES (?, ?, ?, ?, 0) ON CONFLICT DO NOTHING`, workID, lang, title, kind).Error; err != nil {
		t.Fatalf("import alias %q: %v", title, err)
	}
	var row model.CatalogWorkTitle
	if err := testDB.Where("work_id = ? AND lang = ? AND title = ? AND kind = ?",
		workID, lang, title, kind).First(&row).Error; err != nil {
		t.Fatalf("reload alias %q: %v", title, err)
	}
	return row.ID
}

func deleteTitleRow(t *testing.T, id int64) {
	t.Helper()
	if err := testDB.Exec(`DELETE FROM catalog_work_title WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("delete title %d: %v", id, err)
	}
}

func visibleTitles(t *testing.T, workID int64) []string {
	t.Helper()
	var rows []model.CatalogWorkTitle
	if err := testDB.Where("work_id = ?", workID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("load titles: %v", err)
	}
	suppressed, err := editspec.SuppressedWorkTitles(testCtx, testDB, []int64{workID})
	if err != nil {
		t.Fatalf("load suppressions: %v", err)
	}
	set := suppressed[workID]
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if editspec.TitleSuppressed(set, r.Kind, r.Lang, r.Title) {
			continue
		}
		out = append(out, r.Title)
	}
	sort.Strings(out)
	return out
}

func snapshotSuppressed(t *testing.T, e *editing.Engine, workID int64) []string {
	t.Helper()
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, workID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	raw, ok := snap[editspec.FieldWorkTitlesSuppr].([]any)
	if !ok {
		t.Fatalf("snapshot %s = %#v, want a list", editspec.FieldWorkTitlesSuppr, snap[editspec.FieldWorkTitlesSuppr])
	}
	out := make([]string, 0, len(raw))
	for _, el := range raw {
		out = append(out, el.(string))
	}
	return out
}

func suppressList(keys ...string) []any {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	out := make([]any, 0, len(sorted))
	for _, k := range sorted {
		out = append(out, k)
	}
	return out
}

func TestWorkTitleSuppressionEndToEnd(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")
	importAlias(t, work.ID, "ja", "本編タイトル", model.WorkTitleKindOfficial)
	badID := importAlias(t, work.ID, "", "誤った別名", model.WorkTitleKindAlias)
	importAlias(t, work.ID, "", "正しい別名", model.WorkTitleKindAlias)

	editor := realActor(100)
	reviewer := realActor(200, "ren")
	badKey := editspec.WorkTitleIdentity(model.WorkTitleKindAlias, "", "誤った別名")

	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkTitlesSuppr: suppressList(badKey)},
		Note:  "wrong alias pushed by bangumi", Actor: editor,
	})
	if err != nil {
		t.Fatalf("propose suppression: %v", err)
	}
	if rev != nil {
		t.Fatal("a suppression by a contributor without trust or review must queue")
	}
	merged, err := e.MergeProposal(testCtx, prop.ID, reviewer, "confirmed")
	if err != nil {
		t.Fatalf("merge suppression: %v", err)
	}
	if merged.ActorUID != 100 || merged.Site != "nextmoe" {
		t.Fatalf("suppression revision attribution: %+v", merged)
	}

	if got := visibleTitles(t, work.ID); len(got) != 2 || got[0] != "本編タイトル" || got[1] != "正しい別名" {
		t.Fatalf("visible titles after suppression = %v", got)
	}
	if got := snapshotSuppressed(t, e, work.ID); len(got) != 1 || got[0] != badKey {
		t.Fatalf("snapshot suppression set = %v", got)
	}

	// The importer owns the row: deleting it locally and re-pushing the same
	// upstream content must not resurrect it on the read faces.
	deleteTitleRow(t, badID)
	rePushedID := importAlias(t, work.ID, "", "誤った別名", model.WorkTitleKindAlias)
	if rePushedID == badID {
		t.Fatal("re-push reused the row id; the id-independence of the key is untested")
	}
	if got := visibleTitles(t, work.ID); len(got) != 2 {
		t.Fatalf("visible titles after importer re-push = %v", got)
	}

	unsuppressed, unsupRev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkTitlesSuppr: []any{}},
		Note:  "it was right after all", Actor: editor,
	})
	if err != nil {
		t.Fatalf("propose unsuppression: %v", err)
	}
	if unsupRev != nil {
		t.Fatal("unsuppression must go through review too")
	}
	if _, err := e.MergeProposal(testCtx, unsuppressed.ID, reviewer, ""); err != nil {
		t.Fatalf("merge unsuppression: %v", err)
	}
	if got := visibleTitles(t, work.ID); len(got) != 3 {
		t.Fatalf("visible titles after unsuppression = %v", got)
	}
	if got := snapshotSuppressed(t, e, work.ID); len(got) != 0 {
		t.Fatalf("snapshot suppression set after release = %v", got)
	}

	if _, _, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: editspec.TypeWork, EntityID: work.ID, ToSeq: merged.Seq,
		Note: "revert the release", Actor: reviewer,
	}); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if got := visibleTitles(t, work.ID); len(got) != 2 {
		t.Fatalf("visible titles after revert = %v", got)
	}

	revs, err := e.ListRevisions(testCtx, editspec.TypeWork, work.ID, 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(revs) != 3 || revs[0].Action != editing.ActionReverted {
		t.Fatalf("history = %d revisions, newest action %d", len(revs), revs[0].Action)
	}
}

func TestWorkTitleSuppressionAcceptsDanglingRows(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")
	importAlias(t, work.ID, "ja", "本編タイトル", model.WorkTitleKindOfficial)

	key := editspec.WorkTitleIdentity(model.WorkTitleKindAlias, "", "まだ存在しない別名")
	if _, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkTitlesSuppr: suppressList(key)},
		Actor: realActor(100),
	}); err != nil {
		t.Fatalf("suppress a row that does not exist: %v", err)
	} else if rev != nil {
		t.Fatal("a contributor without trust or review must queue")
	}
	props, err := e.ListProposals(testCtx, editing.ProposalFilter{
		EntityType: editspec.TypeWork, EntityID: work.ID, Status: editing.StatusOpen,
	})
	if err != nil || len(props) != 1 {
		t.Fatalf("open proposals = %d err=%v", len(props), err)
	}
	if _, err := e.MergeProposal(testCtx, props[0].ID, realActor(200, "ren"), ""); err != nil {
		t.Fatalf("merge dangling suppression: %v", err)
	}
	if got := visibleTitles(t, work.ID); len(got) != 1 {
		t.Fatalf("visible titles = %v", got)
	}

	importAlias(t, work.ID, "", "まだ存在しない別名", model.WorkTitleKindAlias)
	if got := visibleTitles(t, work.ID); len(got) != 1 || got[0] != "本編タイトル" {
		t.Fatalf("a suppression recorded before the row must bite on arrival: %v", got)
	}
}

func TestWorkTitleIdentityIsContentDerived(t *testing.T) {
	cases := []struct {
		kind  int16
		lang  string
		title string
		want  string
	}{
		{model.WorkTitleKindOfficial, "ja", "サクラノ詩", "title:0:ja:サクラノ詩"},
		{model.WorkTitleKindAlias, "", "Sakura no Uta", "title:1::Sakura no Uta"},
		{model.WorkTitleKindAbbreviation, "zh-Hans", "樱之诗", "title:2:zh-Hans:樱之诗"},
		{model.WorkTitleKindAlias, "", "a:b:c", "title:1::a:b:c"},
		{model.WorkTitleKindSearchHint, "ja", "さくらのうた", "title:3:ja:さくらのうた"},
	}
	for _, c := range cases {
		if got := editspec.WorkTitleIdentity(c.kind, c.lang, c.title); got != c.want {
			t.Fatalf("WorkTitleIdentity(%d, %q, %q) = %q, want %q", c.kind, c.lang, c.title, got, c.want)
		}
	}
}

var dirtyTexts = []struct {
	name, dirty, clean string
}{
	{"zero width space", "\u200bサクラノ詩", "サクラノ詩"},
	{"leading BOM", "\ufeffサクラノ詩", "サクラノ詩"},
	{"bidi override", "\u202eSakura\u202c", "Sakura"},
	{"word joiner", "Sa\u2060kura", "Sakura"},
	{"trailing ascii space", "Sakura no Uta ", "Sakura no Uta"},
	{"leading ideographic space", "\u3000樱之诗", "樱之诗"},
	{"tab and newline ends", "\tSakura\n", "Sakura"},
	{"zero width wrapping a space", "\u200b \u200bSakura\u200b \u200b", "Sakura"},
	{"emoji variation selector survives", "警告\ufe0f", "警告\ufe0f"},
	{"inner spaces survive", "Sakura no Uta", "Sakura no Uta"},
}

func TestWorkTitleIdentityIsCleanStable(t *testing.T) {
	newEngine(t)
	work := createWork(t, "正規化")
	for _, c := range dirtyTexts {
		dirty, clean := editspec.WorkTitleIdentity(model.WorkTitleKindAlias, "ja", c.dirty),
			editspec.WorkTitleIdentity(model.WorkTitleKindAlias, "ja", c.clean)
		if dirty != clean {
			t.Fatalf("%s: key(%q) = %q, key(%q) = %q", c.name, c.dirty, dirty, c.clean, clean)
		}
		if textnorm.Clean(c.dirty) != c.clean {
			t.Fatalf("%s: textnorm.Clean(%q) = %q, want %q", c.name, c.dirty, textnorm.Clean(c.dirty), c.clean)
		}
	}

	// The SQL twin has to reach the same key from the row the importer actually
	// wrote, which is the dirty one — that is what makes the suppression survive
	// cmd/catalog-clean-strings rewriting the column underneath it.
	for i, c := range dirtyTexts {
		id := importAlias(t, work.ID, "ja", c.dirty, model.WorkTitleKindAlias)
		var key string
		if err := testDB.Raw(`SELECT `+editspec.WorkTitleIdentitySQL("t")+
			` FROM catalog_work_title t WHERE t.id = ?`, id).Scan(&key).Error; err != nil {
			t.Fatalf("case %d: compute key in SQL: %v", i, err)
		}
		if want := editspec.WorkTitleIdentity(model.WorkTitleKindAlias, "ja", c.clean); key != want {
			t.Fatalf("%s: SQL key %q, Go key %q", c.name, key, want)
		}
		deleteTitleRow(t, id)
	}
}

func TestWorkTitleSuppressionKeyStableAcrossReimport(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")
	importAlias(t, work.ID, "ja", "本編タイトル", model.WorkTitleKindOfficial)

	first := importAlias(t, work.ID, "", "別名", model.WorkTitleKindAlias)
	var before model.CatalogWorkTitle
	if err := testDB.First(&before, first).Error; err != nil {
		t.Fatal(err)
	}
	keyBefore := editspec.WorkTitleIdentity(before.Kind, before.Lang, before.Title)

	deleteTitleRow(t, first)
	second := importAlias(t, work.ID, "", "別名", model.WorkTitleKindAlias)
	var after model.CatalogWorkTitle
	if err := testDB.First(&after, second).Error; err != nil {
		t.Fatal(err)
	}
	if after.ID == before.ID {
		t.Fatal("the re-imported row kept its id; the test proves nothing")
	}
	keyAfter := editspec.WorkTitleIdentity(after.Kind, after.Lang, after.Title)
	if keyBefore != keyAfter {
		t.Fatalf("identity key drifted across re-import: %q -> %q", keyBefore, keyAfter)
	}
	_ = e
}

func TestWorkTitleIdentitySQLMatchesGo(t *testing.T) {
	newEngine(t)
	work := createWork(t, "SQL 対 Go")
	rows := []model.CatalogWorkTitle{
		{WorkID: work.ID, Lang: "ja", Title: "本編タイトル", Kind: model.WorkTitleKindOfficial},
		{WorkID: work.ID, Lang: "", Title: "コロン:入り:別名", Kind: model.WorkTitleKindAlias},
		{WorkID: work.ID, Lang: "zh-Hans", Title: "带 空格 的 标题 ", Kind: model.WorkTitleKindAbbreviation},
		{WorkID: work.ID, Lang: "ja", Title: "かな", Kind: model.WorkTitleKindSearchHint},
		{WorkID: work.ID, Lang: "en", Title: `quote'and"back\slash`, Kind: model.WorkTitleKindAlias},
	}
	if err := testDB.Create(&rows).Error; err != nil {
		t.Fatalf("create titles: %v", err)
	}

	var got []struct {
		ID    int64  `gorm:"column:id"`
		Lang  string `gorm:"column:lang"`
		Title string `gorm:"column:title"`
		Kind  int16  `gorm:"column:kind"`
		Key   string `gorm:"column:key"`
	}
	if err := testDB.Raw(`SELECT t.id, t.lang, t.title, t.kind, ` +
		editspec.WorkTitleIdentitySQL("t") + ` AS key
		FROM catalog_work_title t WHERE t.work_id = ` +
		fmt.Sprint(work.ID) + ` ORDER BY t.id`).Scan(&got).Error; err != nil {
		t.Fatalf("compute keys in SQL: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("rows = %d, want %d", len(got), len(rows))
	}
	for _, r := range got {
		if want := editspec.WorkTitleIdentity(r.Kind, r.Lang, r.Title); r.Key != want {
			t.Fatalf("row %d: SQL key %q, Go key %q", r.ID, r.Key, want)
		}
	}

	target := got[1]
	if err := testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: work.ID, FieldKey: editspec.FieldWorkTitles,
		IdentityKey: editspec.WorkTitleIdentity(target.Kind, target.Lang, target.Title),
	}).Error; err != nil {
		t.Fatalf("suppress: %v", err)
	}
	var surviving []int64
	if err := testDB.Raw(`SELECT t.id FROM catalog_work_title t
		WHERE t.work_id = ` + fmt.Sprint(work.ID) + ` AND ` +
		editspec.NotSuppressedWorkTitleSQL("t") + ` ORDER BY t.id`).Scan(&surviving).Error; err != nil {
		t.Fatalf("apply predicate: %v", err)
	}
	if len(surviving) != len(rows)-1 {
		t.Fatalf("predicate kept %d rows, want %d", len(surviving), len(rows)-1)
	}
	for _, id := range surviving {
		if id == target.ID {
			t.Fatalf("row %d is suppressed but survived the predicate", id)
		}
	}
}

func TestWorkTitleSuppressionValidation(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")
	editor := realActor(100)

	var valErr *editing.ValidationError
	cases := []any{
		"not-a-list",
		[]any{42.0},
		[]any{""},
		[]any{"title:1::b", "title:1::a"},
		[]any{"title:1::a", "title:1::a"},
	}
	for i, patch := range cases {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkTitlesSuppr: patch}, Actor: editor,
		}); !errors.As(err, &valErr) {
			t.Fatalf("case %d (%#v): err = %v, want ValidationError", i, patch, err)
		}
	}
}

func TestWorkTitleSuppressionInheritsParentPolicy(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterWork(reg, testDB); err != nil {
		t.Fatalf("register: %v", err)
	}
	spec, ok := reg.Type(editspec.TypeWork)
	if !ok {
		t.Fatal("catalog.work is not registered")
	}
	if editspec.FieldWorkTitlesSuppr != editing.SuppressedFieldKey(editspec.FieldWorkTitles) {
		t.Fatalf("companion key %q does not follow the engine's suffix", editspec.FieldWorkTitlesSuppr)
	}
	if parent, companion := spec.EffectivePolicy(editspec.FieldWorkTitles),
		spec.EffectivePolicy(editspec.FieldWorkTitlesSuppr); parent != companion {
		t.Fatalf("parent %+v vs companion %+v", parent, companion)
	}
}
