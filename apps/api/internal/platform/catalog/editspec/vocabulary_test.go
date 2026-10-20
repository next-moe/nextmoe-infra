package editspec_test

import (
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func newVocabEngine(t *testing.T) *editing.Engine {
	t.Helper()
	e := newEngine(t)
	for _, table := range []string{"catalog_character_trait_link", "catalog_character_trait", "catalog_tag_parent"} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	if err := editspec.RegisterTaxonomy(e.Registry(), testDB); err != nil {
		t.Fatalf("register taxonomy: %v", err)
	}
	if err := editspec.RegisterTrait(e.Registry(), testDB); err != nil {
		t.Fatalf("register catalog.trait: %v", err)
	}
	return e
}

func createTag(t *testing.T, name string) int64 {
	t.Helper()
	tag := model.CatalogTag{Name: name, Tier: model.TagTierLongtail, Kind: model.TagKindContent}
	if err := testDB.Create(&tag).Error; err != nil {
		t.Fatalf("create tag %s: %v", name, err)
	}
	return tag.ID
}

func importTagParent(t *testing.T, tagID, parentID int64) {
	t.Helper()
	if err := testDB.Create(&model.CatalogTagParent{TagID: tagID, ParentID: parentID, Origin: model.TagParentOriginVNDB}).Error; err != nil {
		t.Fatalf("import tag parent: %v", err)
	}
}

func tagParentOrigins(t *testing.T, tagID int64) map[int64]int16 {
	t.Helper()
	var rows []model.CatalogTagParent
	if err := testDB.Where("tag_id = ?", tagID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := make(map[int64]int16, len(rows))
	for _, r := range rows {
		out[r.ParentID] = r.Origin
	}
	return out
}

func humanStamped(t *testing.T, table string, id int64, column string) bool {
	t.Helper()
	var human *bool
	if err := testDB.Raw(`SELECT `+editspec.HumanFieldProvenanceSQL("field_provenance", column)+` FROM `+table+` WHERE id = ?`, id).
		Scan(&human).Error; err != nil {
		t.Fatal(err)
	}
	return human != nil && *human
}

func wantValidation(t *testing.T, what string, err error) {
	t.Helper()
	var v *editing.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("%s: want a validation error, got %v", what, err)
	}
}

func TestTagScalarsRoundTripWithStamps(t *testing.T) {
	e := newVocabEngine(t)
	tag := createTag(t, "純愛")
	createTag(t, "NTR")

	if err := mergeEdit(t, e, editspec.TypeTag, tag, map[string]any{
		editspec.FieldTagName:   "纯爱",
		editspec.FieldTagKind:   float64(model.TagKindMeta),
		editspec.FieldTagTier:   float64(model.TagTierCore),
		editspec.FieldTagSexual: true,
		editspec.FieldTagNote:   "以恋爱为主线",
	}); err != nil {
		t.Fatalf("merge tag scalars: %v", err)
	}
	var row model.CatalogTag
	if err := testDB.First(&row, tag).Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "纯爱" || row.Kind != model.TagKindMeta || row.Tier != model.TagTierCore || !row.Sexual || row.Note != "以恋爱为主线" {
		t.Fatalf("tag row: %+v", row)
	}
	for _, col := range []string{"name", "kind", "tier", "sexual", "note"} {
		if !humanStamped(t, "catalog_tag", tag, col) {
			t.Errorf("catalog_tag.%s carries no human stamp", col)
		}
	}

	err := mergeEdit(t, e, editspec.TypeTag, tag, map[string]any{editspec.FieldTagName: "NTR"})
	wantValidation(t, "renaming onto another tag's name", err)
}

func TestTagTierAndSexualNeedAReviewerToPropose(t *testing.T) {
	e := newVocabEngine(t)
	tag := createTag(t, "巨乳")

	for _, field := range []string{editspec.FieldTagTier, editspec.FieldTagSexual} {
		value := any(true)
		if field == editspec.FieldTagTier {
			value = float64(model.TagTierHidden)
		}
		_, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeTag, EntityID: tag, Patch: map[string]any{field: value}, Actor: realActor(1),
		})
		var denied *editing.PermissionError
		if !errors.As(err, &denied) {
			t.Fatalf("%s: a plain user's proposal must be refused, got %v", field, err)
		}
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeTag, EntityID: tag, Patch: map[string]any{field: value}, Actor: realActor(2, "moderator"),
		}); err != nil {
			t.Fatalf("%s: a moderator may propose: %v", field, err)
		}
	}
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeTag, EntityID: tag, Patch: map[string]any{editspec.FieldTagNote: "胸部丰满"}, Actor: realActor(1),
	}); err != nil {
		t.Fatalf("anyone may propose a note: %v", err)
	}
}

func TestTagParentsEndorseDropSuppressAndRefuseCycles(t *testing.T) {
	e := newVocabEngine(t)
	child := createTag(t, "义妹")
	p1, p2, p3 := createTag(t, "妹"), createTag(t, "家人"), createTag(t, "角色属性")
	importTagParent(t, child, p1)
	importTagParent(t, child, p3)

	if err := mergeEdit(t, e, editspec.TypeTag, child, map[string]any{
		editspec.FieldTagParents: []any{float64(p1), float64(p2)},
	}); err != nil {
		t.Fatalf("merge parents: %v", err)
	}
	got := tagParentOrigins(t, child)
	if got[p1] != model.TagParentOriginCurated || got[p2] != model.TagParentOriginCurated || got[p3] != model.TagParentOriginVNDB {
		t.Fatalf("an element endorses the VNDB edge in place and leaves the unnamed one alone: %v", got)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeTag, child)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "tag parents", snap[editspec.FieldTagParents], []any{p1, p2})

	if err := mergeEdit(t, e, editspec.TypeTag, child, map[string]any{
		editspec.FieldTagParents: []any{float64(p2)},
	}); err != nil {
		t.Fatalf("drop a curated parent: %v", err)
	}
	if _, ok := tagParentOrigins(t, child)[p1]; ok {
		t.Fatal("dropping a curated parent deletes its edge")
	}

	if err := mergeEdit(t, e, editspec.TypeTag, child, map[string]any{
		editspec.FieldTagParentsSuppr: []any{editspec.ParentIdentity(p3)},
	}); err != nil {
		t.Fatalf("suppress a VNDB parent: %v", err)
	}
	if _, ok := tagParentOrigins(t, child)[p3]; ok {
		t.Fatal("suppressing a VNDB parent deletes its edge")
	}
	err = mergeEdit(t, e, editspec.TypeTag, child, map[string]any{
		editspec.FieldTagParents: []any{float64(p2), float64(p3)},
	})
	wantValidation(t, "adding a suppressed parent", err)

	err = mergeEdit(t, e, editspec.TypeTag, p2, map[string]any{editspec.FieldTagParents: []any{float64(child)}})
	wantValidation(t, "a parent edge closing a loop", err)
	err = mergeEdit(t, e, editspec.TypeTag, child, map[string]any{editspec.FieldTagParents: []any{float64(child)}})
	wantValidation(t, "a tag as its own parent", err)
}

func createVocabTrait(t *testing.T, tid, name, group string) int64 {
	t.Helper()
	tr := model.CatalogCharacterTrait{VndbTID: tid, Name: name, GroupTID: group, Searchable: true, Applicable: group != ""}
	if err := testDB.Create(&tr).Error; err != nil {
		t.Fatalf("create trait %s: %v", tid, err)
	}
	return tr.ID
}

func importTraitParent(t *testing.T, traitID, parentID int64) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO catalog_character_trait_parent (trait_id, parent_id) VALUES (?, ?)`, traitID, parentID).Error; err != nil {
		t.Fatalf("import trait parent: %v", err)
	}
}

func traitRow(t *testing.T, id int64) model.CatalogCharacterTrait {
	t.Helper()
	var row model.CatalogCharacterTrait
	if err := testDB.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestTraitFieldsRoundTripWithStampsAndSexualFamily(t *testing.T) {
	e := newVocabEngine(t)
	hair := createVocabTrait(t, "i1", "Hair", "")
	body := createVocabTrait(t, "i2", "Body", "")
	blond := createVocabTrait(t, "i10", "Blond", "i1")
	golden := createVocabTrait(t, "i11", "Golden Blond", "i1")
	importTraitParent(t, golden, blond)

	if err := mergeEdit(t, e, editspec.TypeTrait, blond, map[string]any{
		editspec.FieldTraitName:           "Blond Hair",
		editspec.FieldTraitNameZh:         "金发",
		editspec.FieldTraitAliases:        []any{"Golden", "Yellow"},
		editspec.FieldTraitDescriptionZh:  "金色的头发",
		editspec.FieldTraitGroup:          float64(body),
		editspec.FieldTraitGroupOrder:     float64(5),
		editspec.FieldTraitDefaultSpoiler: float64(1),
		editspec.FieldTraitSearchable:     false,
		editspec.FieldTraitSexual:         true,
	}); err != nil {
		t.Fatalf("merge trait fields: %v", err)
	}
	row := traitRow(t, blond)
	if row.Name != "Blond Hair" || row.NameZh != "金发" || row.NameZhProvenance != model.TraitNameZhProvenanceCurated ||
		row.Alias != "Golden\nYellow" || row.DescriptionZh != "金色的头发" || row.GroupTID != "i2" || row.GOrder != 5 ||
		row.DefaultSpoil != 1 || row.Searchable || !row.Sexual || !row.SexualFamily {
		t.Fatalf("trait row: %+v", row)
	}
	if !traitRow(t, golden).SexualFamily {
		t.Fatal("a child of a trait made sexual joins the sexual family")
	}
	for _, col := range []string{"name", "name_zh", "alias", "description_zh", "group_tid", "gorder", "default_spoil", "searchable", "sexual"} {
		if !humanStamped(t, "catalog_character_trait", blond, col) {
			t.Errorf("catalog_character_trait.%s carries no human stamp", col)
		}
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeTrait, blond)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "trait group", snap[editspec.FieldTraitGroup], body)
	sameJSON(t, "trait aliases", snap[editspec.FieldTraitAliases], []any{"Golden", "Yellow"})

	err = mergeEdit(t, e, editspec.TypeTrait, golden, map[string]any{editspec.FieldTraitGroup: float64(blond)})
	wantValidation(t, "a group that is itself in a group", err)
	err = mergeEdit(t, e, editspec.TypeTrait, hair, map[string]any{editspec.FieldTraitGroup: float64(body)})
	wantValidation(t, "a group with members joining a group", err)
}

func TestTraitParentsSuppressionRederivesTheSexualFamily(t *testing.T) {
	e := newVocabEngine(t)
	createVocabTrait(t, "i1", "Body", "")
	lewd := createVocabTrait(t, "i10", "Lewd", "i1")
	child := createVocabTrait(t, "i11", "Lewd Child", "i1")
	if err := testDB.Model(&model.CatalogCharacterTrait{}).Where("id = ?", lewd).Update("sexual", true).Error; err != nil {
		t.Fatal(err)
	}
	importTraitParent(t, child, lewd)
	if err := testDB.Exec(model.TraitSexualFamilySQL).Error; err != nil {
		t.Fatal(err)
	}
	if !traitRow(t, child).SexualFamily {
		t.Fatal("setup: the child descends from a sexual trait")
	}

	if err := mergeEdit(t, e, editspec.TypeTrait, child, map[string]any{
		editspec.FieldTraitParentsSuppr: []any{editspec.ParentIdentity(lewd)},
	}); err != nil {
		t.Fatalf("suppress the VNDB parent: %v", err)
	}
	var edges int64
	if err := testDB.Model(&model.CatalogCharacterTraitParent{}).Where("trait_id = ?", child).Count(&edges).Error; err != nil {
		t.Fatal(err)
	}
	if edges != 0 || traitRow(t, child).SexualFamily {
		t.Fatalf("the suppressed edge is gone (%d left) and the child leaves the sexual family", edges)
	}

	if err := mergeEdit(t, e, editspec.TypeTrait, lewd, map[string]any{
		editspec.FieldTraitParents: []any{float64(child)},
	}); err != nil {
		t.Fatalf("curated parent: %v", err)
	}
	err := mergeEdit(t, e, editspec.TypeTrait, child, map[string]any{editspec.FieldTraitParentsSuppr: []any{}})
	if err != nil {
		t.Fatalf("lift the suppression: %v", err)
	}
	err = mergeEdit(t, e, editspec.TypeTrait, child, map[string]any{editspec.FieldTraitParents: []any{float64(lewd)}})
	wantValidation(t, "a trait parent edge closing a loop", err)
}
