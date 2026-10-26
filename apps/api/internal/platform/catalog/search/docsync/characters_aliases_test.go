package docsync

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func TestSuppressedCharacterAliasLeavesTheSearchDocument(t *testing.T) {
	for _, tbl := range []string{"catalog_character_alias", "catalog_character", "edit_suppressed_row"} {
		if err := facetTestDB.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}

	ch := model.CatalogCharacter{DisplayName: "主人公"}
	if err := facetTestDB.Create(&ch).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	rows := []model.CatalogCharacterAlias{
		{CharacterID: ch.ID, Name: "しゅじんこう", Lang: "ja", Kind: model.AliasKindSpellingVariant},
		{CharacterID: ch.ID, Name: "誤った別名", Lang: "ja", Kind: model.AliasKindSpellingVariant},
	}
	if err := facetTestDB.Create(&rows).Error; err != nil {
		t.Fatalf("create aliases: %v", err)
	}

	live := editspec.NotSuppressedCharacterAliasSQL("a")
	before, err := loadAliasTable(facetTestDB, "catalog_character_alias", "character_id", live, nil)
	if err != nil {
		t.Fatalf("loadAliasTable: %v", err)
	}
	if len(before[ch.ID]) != 2 {
		t.Fatalf("aliases before suppression = %v", before[ch.ID])
	}

	if err := facetTestDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeCharacter, EntityID: ch.ID, FieldKey: editspec.FieldCharacterAliases,
		IdentityKey: editspec.CharacterAliasIdentity(model.AliasKindSpellingVariant, "ja", "誤った別名"),
	}).Error; err != nil {
		t.Fatalf("suppress: %v", err)
	}

	after, err := loadAliasTable(facetTestDB, "catalog_character_alias", "character_id", live, nil)
	if err != nil {
		t.Fatalf("loadAliasTable: %v", err)
	}
	if len(after[ch.ID]) != 1 || after[ch.ID][0].name != "しゅじんこう" {
		t.Fatalf("aliases after suppression = %v, want the suppressed one out of the index input", after[ch.ID])
	}
}
