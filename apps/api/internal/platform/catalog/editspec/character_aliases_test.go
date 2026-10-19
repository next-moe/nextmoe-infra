package editspec_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

// importCharacterAlias reproduces importer/rostervndb.go's insert verbatim: the
// row arrives from upstream with a fresh identity column every time, which is
// why the suppression key may never be derived from it.
func importCharacterAlias(t *testing.T, characterID int64, name, lang string, kind int16, sourceID int16) int64 {
	t.Helper()
	if err := testDB.Exec(
		`INSERT INTO catalog_character_alias (character_id, name, lang, kind, is_primary_for_locale, source_id, provenance)
		 VALUES (?, ?, ?, ?, false, ?, 0) ON CONFLICT DO NOTHING`,
		characterID, name, lang, kind, sourceID).Error; err != nil {
		t.Fatalf("import alias %q: %v", name, err)
	}
	var row model.CatalogCharacterAlias
	if err := testDB.Where("character_id = ? AND name = ? AND lang = ?", characterID, name, lang).
		First(&row).Error; err != nil {
		t.Fatalf("reload alias %q: %v", name, err)
	}
	return row.ID
}

func liveAliasNames(t *testing.T, characterID int64) []string {
	t.Helper()
	var names []string
	if err := testDB.Raw(`SELECT a.name FROM catalog_character_alias a
		WHERE a.character_id = ? AND `+editspec.NotSuppressedCharacterAliasSQL("a")+`
		ORDER BY a.id`, characterID).Scan(&names).Error; err != nil {
		t.Fatalf("apply predicate: %v", err)
	}
	return names
}

func TestCharacterAliasSuppressionEndToEnd(t *testing.T) {
	e := newCharacterEngine(t)
	ch := createCharacter(t, "主人公")
	importCharacterAlias(t, ch.ID, "しゅじんこう", "ja", model.AliasKindSpellingVariant, 2)
	badID := importCharacterAlias(t, ch.ID, "誤った別名", "ja", model.AliasKindSpellingVariant, 2)

	editor := realActor(100, "admin")
	reviewer := realActor(200, "ren")
	badKey := editspec.CharacterAliasIdentity(model.AliasKindSpellingVariant, "ja", "誤った別名")

	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeCharacter, EntityID: ch.ID,
		Patch: map[string]any{editspec.FieldCharacterAliasesSuppr: suppressList(badKey)},
		Note:  "wrong alias pushed by the vndb roster", Actor: editor,
	})
	if err != nil {
		t.Fatalf("propose suppression: %v", err)
	}
	if rev != nil {
		t.Fatal("catalog.character.aliases.suppressed must inherit automerge=never on nextmoe")
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, reviewer, "confirmed"); err != nil {
		t.Fatalf("merge suppression: %v", err)
	}

	if got := liveAliasNames(t, ch.ID); len(got) != 1 || got[0] != "しゅじんこう" {
		t.Fatalf("live aliases after suppression = %v", got)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeCharacter, ch.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sameJSON(t, "suppression set", snap[editspec.FieldCharacterAliasesSuppr], []any{badKey})

	// The roster importer owns the row: deleting it locally and re-pushing the
	// same upstream content must not resurrect it on the read faces.
	if err := testDB.Exec(`DELETE FROM catalog_character_alias WHERE id = ?`, badID).Error; err != nil {
		t.Fatal(err)
	}
	rePushed := importCharacterAlias(t, ch.ID, "誤った別名", "ja", model.AliasKindSpellingVariant, 2)
	if rePushed == badID {
		t.Fatal("the re-push reused the row id; the id-independence of the key is untested")
	}
	if got := liveAliasNames(t, ch.ID); len(got) != 1 {
		t.Fatalf("live aliases after the importer re-push = %v", got)
	}

	release, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeCharacter, EntityID: ch.ID,
		Patch: map[string]any{editspec.FieldCharacterAliasesSuppr: []any{}},
		Note:  "it was right after all", Actor: editor,
	})
	if err != nil {
		t.Fatalf("propose unsuppression: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, release.ID, reviewer, ""); err != nil {
		t.Fatalf("merge unsuppression: %v", err)
	}
	if got := liveAliasNames(t, ch.ID); len(got) != 2 {
		t.Fatalf("live aliases after release = %v", got)
	}
}

func TestCharacterAliasIdentityIsContentDerived(t *testing.T) {
	cases := []struct {
		kind       int16
		lang, name string
		want       string
	}{
		{model.AliasKindTranslation, "zh-Hans", "绪方", "alias:0:zh-Hans:绪方"},
		{model.AliasKindSpellingVariant, "ja", "おがた", "alias:1:ja:おがた"},
		{model.AliasKindSpellingVariant, "en", "a:b:c", "alias:1:en:a:b:c"},
		{model.AliasKindSearchHint, "", "hint", "alias:2::hint"},
	}
	for _, c := range cases {
		if got := editspec.CharacterAliasIdentity(c.kind, c.lang, c.name); got != c.want {
			t.Fatalf("CharacterAliasIdentity(%d, %q, %q) = %q, want %q", c.kind, c.lang, c.name, got, c.want)
		}
	}
}

func TestCharacterAliasIdentityIsCleanStable(t *testing.T) {
	newCharacterEngine(t)
	ch := createCharacter(t, "正規化")
	for _, c := range dirtyTexts {
		dirty, clean := editspec.CharacterAliasIdentity(model.AliasKindTranslation, "ja", c.dirty),
			editspec.CharacterAliasIdentity(model.AliasKindTranslation, "ja", c.clean)
		if dirty != clean {
			t.Fatalf("%s: key(%q) = %q, key(%q) = %q", c.name, c.dirty, dirty, c.clean, clean)
		}
	}

	for i, c := range dirtyTexts {
		row := model.CatalogCharacterAlias{
			CharacterID: ch.ID, Name: c.dirty, Lang: "ja", Kind: model.AliasKindTranslation,
		}
		if err := testDB.Create(&row).Error; err != nil {
			t.Fatalf("case %d: insert %q: %v", i, c.dirty, err)
		}
		var key string
		if err := testDB.Raw(`SELECT `+editspec.CharacterAliasIdentitySQL("a")+
			` FROM catalog_character_alias a WHERE a.id = ?`, row.ID).Scan(&key).Error; err != nil {
			t.Fatalf("case %d: compute key in SQL: %v", i, err)
		}
		if want := editspec.CharacterAliasIdentity(model.AliasKindTranslation, "ja", c.clean); key != want {
			t.Fatalf("%s: SQL key %q, Go key %q", c.name, key, want)
		}
		if err := testDB.Exec(`DELETE FROM catalog_character_alias WHERE id = ?`, row.ID).Error; err != nil {
			t.Fatalf("case %d: cleanup: %v", i, err)
		}
	}
}

func TestCharacterAliasIdentitySQLMatchesGo(t *testing.T) {
	newCharacterEngine(t)
	ch := createCharacter(t, "SQL 対 Go")
	rows := []model.CatalogCharacterAlias{
		{CharacterID: ch.ID, Name: "しゅじんこう", Lang: "ja", Kind: model.AliasKindSpellingVariant},
		{CharacterID: ch.ID, Name: "コロン:入り:別名", Lang: "ja", Kind: model.AliasKindTranslation},
		{CharacterID: ch.ID, Name: "带 空格 的 名字 ", Lang: "zh-Hans", Kind: model.AliasKindTranslation},
		{CharacterID: ch.ID, Name: `quote'and"back\slash`, Lang: "en", Kind: model.AliasKindSpellingVariant},
		{CharacterID: ch.ID, Name: "けんさくヒント", Lang: "", Kind: model.AliasKindSearchHint},
	}
	if err := testDB.Create(&rows).Error; err != nil {
		t.Fatalf("create aliases: %v", err)
	}

	var got []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
		Lang string `gorm:"column:lang"`
		Kind int16  `gorm:"column:kind"`
		Key  string `gorm:"column:key"`
	}
	if err := testDB.Raw(`SELECT a.id, a.name, a.lang, a.kind, ` +
		editspec.CharacterAliasIdentitySQL("a") + ` AS key
		FROM catalog_character_alias a WHERE a.character_id = ` +
		fmt.Sprint(ch.ID) + ` ORDER BY a.id`).Scan(&got).Error; err != nil {
		t.Fatalf("compute keys in SQL: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("rows = %d, want %d", len(got), len(rows))
	}
	for _, r := range got {
		if want := editspec.CharacterAliasIdentity(r.Kind, r.Lang, r.Name); r.Key != want {
			t.Fatalf("row %d: SQL key %q, Go key %q", r.ID, r.Key, want)
		}
	}

	target := got[1]
	if err := testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeCharacter, EntityID: ch.ID, FieldKey: editspec.FieldCharacterAliases,
		IdentityKey: editspec.CharacterAliasIdentity(target.Kind, target.Lang, target.Name),
	}).Error; err != nil {
		t.Fatalf("suppress: %v", err)
	}
	surviving := liveAliasNames(t, ch.ID)
	if len(surviving) != len(rows)-1 {
		t.Fatalf("predicate kept %d rows, want %d", len(surviving), len(rows)-1)
	}
	for _, name := range surviving {
		if name == target.Name {
			t.Fatalf("row %q is suppressed but survived the predicate", name)
		}
	}
}

func TestCuratedAliasCollidingWithUpstreamIsRejected(t *testing.T) {
	e := newCharacterEngine(t)
	ch := createCharacter(t, "衝突テスト")
	importCharacterAlias(t, ch.ID, "上流の別名", "zh-Hans", model.AliasKindTranslation, 3)

	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeCharacter, EntityID: ch.ID,
		Patch: map[string]any{editspec.FieldCharacterAliases: []any{
			map[string]any{"name": "上流の別名", "lang": "zh-Hans", "kind": float64(model.AliasKindTranslation)},
		}},
		Actor: realActor(100, "admin"),
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	_, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "")
	var valErr *editing.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("merge a curated alias colliding with an upstream row: %v, want ValidationError (422)", err)
	}
	if !strings.Contains(valErr.Error(), editspec.FieldCharacterAliasesSuppr) {
		t.Fatalf("the 422 must point at the suppression field, got %q", valErr.Error())
	}

	// The failed merge must not have eaten the upstream row on its way out.
	var n int64
	if err := testDB.Model(&model.CatalogCharacterAlias{}).
		Where("character_id = ?", ch.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("alias rows after the rejected merge = %d, want the upstream row untouched", n)
	}
}

func TestCuratedAliasLaneRoundTrips(t *testing.T) {
	e := newCharacterEngine(t)
	ch := createCharacter(t, "别名车道")
	importCharacterAlias(t, ch.ID, "上流だけの別名", "ja", model.AliasKindSpellingVariant, 2)

	aliases := []any{
		map[string]any{"name": "人手の別名", "lang": "zh-Hans", "kind": float64(model.AliasKindTranslation), "primary": true},
		map[string]any{"name": "Human Alias", "lang": "en", "kind": float64(model.AliasKindSpellingVariant), "latin": "Human Alias"},
	}
	snap := mergeOn(t, e, editspec.TypeCharacter, ch.ID,
		map[string]any{editspec.FieldCharacterAliases: aliases})
	sameJSON(t, "curated aliases", snap[editspec.FieldCharacterAliases], aliases)

	// Only the curated lane is in the snapshot; the upstream row keeps its own
	// place in the table and is reachable only through .suppressed.
	var all []model.CatalogCharacterAlias
	if err := testDB.Where("character_id = ?", ch.ID).Order("id").Find(&all).Error; err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("alias rows = %d, want the upstream row plus the two curated ones", len(all))
	}
	for _, a := range all {
		if a.SourceID != nil && *a.SourceID == 12 && a.Provenance != model.AliasProvenanceSource {
			t.Fatalf("a curated alias must carry provenance=source: %+v", a)
		}
	}

	// Removing an element from the list deletes only the curated row.
	mergeOn(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterAliases: []any{aliases[0]},
	})
	if err := testDB.Where("character_id = ?", ch.ID).Order("id").Find(&all).Error; err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("alias rows after the removal = %+v", all)
	}
}

func TestCharacterAliasValidation(t *testing.T) {
	e := newCharacterEngine(t)
	ch := createCharacter(t, "検証")
	editor := realActor(100, "admin")

	var valErr *editing.ValidationError
	cases := []any{
		"not-a-list",
		[]any{map[string]any{"lang": "ja", "kind": float64(1)}},
		[]any{map[string]any{"name": "x", "kind": float64(1)}},
		[]any{map[string]any{"name": "x", "lang": "", "kind": float64(1)}},
		[]any{map[string]any{"name": "x", "lang": "xx", "kind": float64(1)}},
		[]any{map[string]any{"name": "x", "lang": "ja", "kind": float64(model.AliasKindSearchHint)}},
		[]any{map[string]any{"name": "x", "lang": "ja", "kind": float64(1), "unknown": "y"}},
		[]any{
			map[string]any{"name": "x", "lang": "ja", "kind": float64(0)},
			map[string]any{"name": "x", "lang": "ja", "kind": float64(1)},
		},
	}
	for i, patch := range cases {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeCharacter, EntityID: ch.ID,
			Patch: map[string]any{editspec.FieldCharacterAliases: patch}, Actor: editor,
		}); !errors.As(err, &valErr) {
			t.Errorf("case %d (%#v): err = %v, want ValidationError", i, patch, err)
		}
	}
}

func TestCuratedSearchHintSurvivesAliasReplace(t *testing.T) {
	e := newCharacterEngine(t)
	ch := createCharacter(t, "ヒント保護")
	src := int16(12)
	hint := model.CatalogCharacterAlias{
		CharacterID: ch.ID, Name: "けんさくヒント", Lang: "",
		Kind: model.AliasKindSearchHint, SourceID: &src,
		Provenance: model.AliasProvenanceSource,
	}
	if err := testDB.Create(&hint).Error; err != nil {
		t.Fatalf("seed search hint: %v", err)
	}

	old := []any{map[string]any{
		"name": "古い別名", "lang": "zh-Hans", "kind": float64(model.AliasKindTranslation),
	}}
	mergeOn(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterAliases: old,
	})
	var still model.CatalogCharacterAlias
	if err := testDB.First(&still, hint.ID).Error; err != nil {
		t.Fatalf("the first save deleted the search-hint row: %v", err)
	}

	newer := []any{map[string]any{
		"name": "新しい別名", "lang": "zh-Hans", "kind": float64(model.AliasKindTranslation),
	}}
	snap := mergeOn(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterAliases: newer,
	})
	sameJSON(t, "curated aliases", snap[editspec.FieldCharacterAliases], newer)

	if err := testDB.First(&still, hint.ID).Error; err != nil {
		t.Fatalf("the replace deleted the search-hint row: %v", err)
	}
	if still.Kind != model.AliasKindSearchHint || still.Name != "けんさくヒント" {
		t.Fatalf("search hint was rewritten: %+v", still)
	}

	var rows []model.CatalogCharacterAlias
	if err := testDB.Where("character_id = ?", ch.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("alias rows = %+v, want the search hint plus the new curated row", rows)
	}
	var sawOld, sawNew bool
	for _, r := range rows {
		if r.Name == "古い別名" {
			sawOld = true
		}
		if r.Name == "新しい別名" {
			sawNew = true
			if r.SourceID == nil || *r.SourceID != 12 {
				t.Fatalf("replacement curated row source = %v, want 12", r.SourceID)
			}
		}
	}
	if sawOld {
		t.Fatal("the previous non-hint curated alias must be replaced")
	}
	if !sawNew {
		t.Fatal("the new curated alias is missing")
	}
}

func TestCharacterAliasSuppressionInheritsParentPolicy(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterCharacter(reg, testDB); err != nil {
		t.Fatalf("register: %v", err)
	}
	spec, ok := reg.Type(editspec.TypeCharacter)
	if !ok {
		t.Fatal("catalog.character is not registered")
	}
	if editspec.FieldCharacterAliasesSuppr != editing.SuppressedFieldKey(editspec.FieldCharacterAliases) {
		t.Fatalf("companion key %q does not follow the engine's suffix", editspec.FieldCharacterAliasesSuppr)
	}
	parent := spec.EffectivePolicy(editspec.FieldCharacterAliases)
	companion := spec.EffectivePolicy(editspec.FieldCharacterAliasesSuppr)
	if parent != companion {
		t.Fatalf("parent %+v vs companion %+v", parent, companion)
	}
	locked := editing.ProposeLocked
	if skipped := reg.SetPolicyOverrides([]editing.PolicyOverride{
		{EntityType: editspec.TypeCharacter, FieldKey: editspec.FieldCharacterAliases, Propose: &locked},
	}); len(skipped) > 0 {
		t.Fatal(skipped)
	}
	if got := spec.EffectivePolicy(editspec.FieldCharacterAliasesSuppr); got.Propose != editing.ProposeLocked {
		t.Fatalf("an override on the parent must reach the companion: %+v", got)
	}
}
