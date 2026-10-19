package editspec_test

import (
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

func newTraitEngine(t *testing.T) *editing.Engine {
	t.Helper()
	e := newCharacterEngine(t)
	for _, table := range []string{"catalog_character_trait_link", "catalog_character_trait"} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	return e
}

func createTrait(t *testing.T, tid, name string, applicable bool) int64 {
	t.Helper()
	tr := model.CatalogCharacterTrait{
		VndbTID: tid, Name: name, GroupTID: "i1", Searchable: true, Applicable: applicable,
	}
	if err := testDB.Create(&tr).Error; err != nil {
		t.Fatalf("create trait %s: %v", tid, err)
	}
	return tr.ID
}

// importTraitLink is chartraits' insert, which never names source_id.
func importTraitLink(t *testing.T, characterID, traitID int64, spoiler int16) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO catalog_character_trait_link (character_id, trait_id, spoiler_level, lie)
		VALUES (?, ?, ?, false)`, characterID, traitID, spoiler).Error; err != nil {
		t.Fatalf("import trait link: %v", err)
	}
}

func traitLinks(t *testing.T, characterID int64) map[int64]model.CatalogCharacterTraitLink {
	t.Helper()
	var rows []model.CatalogCharacterTraitLink
	if err := testDB.Where("character_id = ?", characterID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := make(map[int64]model.CatalogCharacterTraitLink, len(rows))
	for _, r := range rows {
		out[r.TraitID] = r
	}
	return out
}

func mergeEdit(t *testing.T, e *editing.Engine, entityType string, id int64, patch map[string]any) error {
	t.Helper()
	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: entityType, EntityID: id, Patch: patch, Note: "test", Actor: realActor(100, "admin"),
	})
	if err != nil {
		return err
	}
	if rev != nil {
		t.Fatalf("%s must not automerge", entityType)
	}
	_, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "ok")
	return err
}

func traitEl(id int64, spoiler int, lie bool) map[string]any {
	return map[string]any{"trait_id": float64(id), "spoiler": float64(spoiler), "lie": lie}
}

func TestCharacterTraitsTakeOverUpstreamAndRoundTrip(t *testing.T) {
	e := newTraitEngine(t)
	ch := createCharacter(t, "主人公")
	blond := createTrait(t, "i10", "Blond Hair", true)
	long := createTrait(t, "i11", "Long Hair", true)
	short := createTrait(t, "i12", "Short", true)
	importTraitLink(t, ch.ID, blond, 0)
	importTraitLink(t, ch.ID, short, 0)

	if links := traitLinks(t, ch.ID); links[blond].SourceID != 2 {
		t.Fatalf("an imported link defaults to vndb, got %d", links[blond].SourceID)
	}
	if err := mergeEdit(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterTraits: []any{traitEl(blond, 2, true), traitEl(long, 0, false)},
	}); err != nil {
		t.Fatalf("merge traits: %v", err)
	}
	links := traitLinks(t, ch.ID)
	if l := links[blond]; l.SourceID != 12 || l.SpoilerLevel != 2 || !l.Lie {
		t.Fatalf("the curated element must take the upstream row over: %+v", l)
	}
	if l := links[long]; l.SourceID != 12 {
		t.Fatalf("a new curated link: %+v", l)
	}
	if l := links[short]; l.SourceID != 2 {
		t.Fatalf("an upstream link the edit never named stays upstream: %+v", l)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeCharacter, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "traits", snap[editspec.FieldCharacterTraits], []any{
		map[string]any{"lie": true, "spoiler": 2, "trait_id": blond},
		map[string]any{"lie": false, "spoiler": 0, "trait_id": long},
	})

	if err := mergeEdit(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterTraits: []any{traitEl(blond, 2, true)},
	}); err != nil {
		t.Fatalf("drop a curated link: %v", err)
	}
	if _, ok := traitLinks(t, ch.ID)[long]; ok {
		t.Fatal("dropping a curated element deletes its row")
	}
}

func TestCharacterTraitSuppressionPurgesUpstreamAndRejectsContradiction(t *testing.T) {
	e := newTraitEngine(t)
	ch := createCharacter(t, "主人公")
	wrong := createTrait(t, "i20", "Wrong", true)
	importTraitLink(t, ch.ID, wrong, 1)

	key := editspec.CharacterTraitIdentity(wrong)
	if err := mergeEdit(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterTraitsSuppr: suppressList(key),
	}); err != nil {
		t.Fatalf("suppress: %v", err)
	}
	if _, ok := traitLinks(t, ch.ID)[wrong]; ok {
		t.Fatal("suppression must remove the upstream link")
	}
	err := mergeEdit(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterTraits: []any{traitEl(wrong, 0, false)},
	})
	var ve *editing.ValidationError
	if !errors.As(err, &ve) || ve.Key != editspec.FieldCharacterTraits {
		t.Fatalf("assigning a suppressed trait must be a 422 on traits, got %v", err)
	}
}

func TestCharacterTraitValidation(t *testing.T) {
	e := newTraitEngine(t)
	ch := createCharacter(t, "主人公")
	group := createTrait(t, "i1", "Hair", false)
	for name, patch := range map[string][]any{
		"missing lie":     {map[string]any{"trait_id": float64(5), "spoiler": float64(0)}},
		"spoiler range":   {traitEl(5, 3, false)},
		"duplicate trait": {traitEl(5, 0, false), traitEl(5, 1, false)},
	} {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeCharacter, EntityID: ch.ID,
			Patch: map[string]any{editspec.FieldCharacterTraits: patch}, Actor: realActor(100, "admin"),
		}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, id := range map[string]int64{"group heading": group, "unknown trait": 99999} {
		err := mergeEdit(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
			editspec.FieldCharacterTraits: []any{traitEl(id, 0, false)},
		})
		var ve *editing.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: want a validation error, got %v", name, err)
		}
	}
}

func TestCharacterPortraitEditClearsTheCutoutSourceAndStamps(t *testing.T) {
	e := newTraitEngine(t)
	ch := createCharacter(t, "主人公")
	if err := testDB.Model(&model.CatalogCharacter{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"image_hash": "cutout", "image_source_hash": "original",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := mergeEdit(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterImage: "handpicked",
	}); err != nil {
		t.Fatalf("set portrait: %v", err)
	}
	got := reloadCharacter(t, ch.ID)
	if got.ImageHash == nil || *got.ImageHash != "handpicked" || got.ImageSourceHash != nil {
		t.Fatalf("image=%v source=%v", got.ImageHash, got.ImageSourceHash)
	}
	if src := provenance.FirstSource(got.FieldProvenance, "image_hash"); !provenance.IsHuman(src) {
		t.Fatalf("image_hash stamp = %q", src)
	}
	if err := mergeEdit(t, e, editspec.TypeCharacter, ch.ID, map[string]any{
		editspec.FieldCharacterImage: nil,
	}); err != nil {
		t.Fatalf("clear portrait: %v", err)
	}
	if got := reloadCharacter(t, ch.ID); got.ImageHash != nil {
		t.Fatal("null clears the portrait")
	}
}

// The portrait, cut-out, getchu-portrait, label-logo and instance backfills all
// splice NotHumanFieldSQL into their fill-only UPDATE; this pins what it lets
// through, including the unstamped row a bare NOT would have skipped.
func TestNotHumanFieldSQLGuardsFillOnlyWriters(t *testing.T) {
	e := newTraitEngine(t)
	unstamped := createCharacter(t, "未標記")
	machine := createCharacter(t, "機械")
	cleared := createCharacter(t, "人が消した")
	if err := testDB.Exec(`UPDATE catalog_character SET field_provenance = '{"image_hash":[{"source":"vndb","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`, machine.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Model(&model.CatalogCharacter{}).Where("id = ?", cleared.ID).Update("image_hash", "old").Error; err != nil {
		t.Fatal(err)
	}
	if err := mergeEdit(t, e, editspec.TypeCharacter, cleared.ID, map[string]any{editspec.FieldCharacterImage: nil}); err != nil {
		t.Fatalf("clear portrait: %v", err)
	}
	res := testDB.Exec(`UPDATE catalog_character SET image_hash = 'machine-fill'
		WHERE id IN ? AND image_hash IS NULL AND `+editspec.NotHumanFieldSQL("field_provenance", "image_hash"),
		[]int64{unstamped.ID, machine.ID, cleared.ID})
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	if res.RowsAffected != 2 {
		t.Fatalf("filled %d rows, want the unstamped and the machine-stamped one", res.RowsAffected)
	}
	if got := reloadCharacter(t, cleared.ID); got.ImageHash != nil {
		t.Fatalf("a portrait a person cleared was refilled with %q", *got.ImageHash)
	}
}

func TestCharacterInstanceOfRules(t *testing.T) {
	e := newTraitEngine(t)
	main := createCharacter(t, "本体")
	inst := createCharacter(t, "変身")
	other := createCharacter(t, "別人")
	if err := mergeEdit(t, e, editspec.TypeCharacter, inst.ID, map[string]any{
		editspec.FieldCharacterInstanceOf: float64(main.ID),
	}); err != nil {
		t.Fatalf("set instance_of: %v", err)
	}
	if got := reloadCharacter(t, inst.ID); got.InstanceOf == nil || *got.InstanceOf != main.ID {
		t.Fatalf("instance_of = %v", got.InstanceOf)
	}
	cases := map[string]struct {
		id     int64
		target int64
	}{
		"self":              {other.ID, other.ID},
		"chain":             {other.ID, inst.ID},
		"main becomes inst": {main.ID, other.ID},
		"missing target":    {other.ID, 99999},
	}
	for name, c := range cases {
		err := mergeEdit(t, e, editspec.TypeCharacter, c.id, map[string]any{
			editspec.FieldCharacterInstanceOf: float64(c.target),
		})
		var ve *editing.ValidationError
		if !errors.As(err, &ve) || ve.Key != editspec.FieldCharacterInstanceOf {
			t.Errorf("%s: want a 422 on instance_of, got %v", name, err)
		}
	}
}
