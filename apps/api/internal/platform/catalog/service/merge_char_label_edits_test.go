package service

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkTrait(t *testing.T, tid string) int64 {
	t.Helper()
	tr := model.CatalogCharacterTrait{VndbTID: tid, Name: tid, GroupTID: "i1", Searchable: true, Applicable: true}
	require.NoError(t, testDB.Create(&tr).Error)
	return tr.ID
}

func mkTraitLink(t *testing.T, characterID, traitID int64, spoiler int16, source int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_character_trait_link
		(character_id, trait_id, spoiler_level, lie, source_id, created_at, updated_at)
		VALUES (?, ?, ?, false, ?, now(), now())`, characterID, traitID, spoiler, source).Error)
}

func suppressRow(t *testing.T, entityType string, entityID int64, field, key string) {
	t.Helper()
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: entityType, EntityID: entityID, FieldKey: field, IdentityKey: key,
	}).Error)
}

func TestCharacterMergeKeepsCuratedTraitsAndInstances(t *testing.T) {
	cleanTables(t)
	src, dst := createCharacter(t, "旧"), createCharacter(t, "新")
	inst := createCharacter(t, "変身")
	require.NoError(t, testDB.Model(&model.CatalogCharacter{}).Where("id = ?", inst.ID).Update("instance_of", src.ID).Error)

	regraded, hidden := mkTrait(t, "i10"), mkTrait(t, "i11")
	mkTraitLink(t, src.ID, regraded, 2, 12)
	mkTraitLink(t, dst.ID, regraded, 0, 2)
	mkTraitLink(t, dst.ID, hidden, 0, 2)
	suppressRow(t, editspec.TypeCharacter, src.ID, editspec.FieldCharacterTraits, editspec.CharacterTraitIdentity(hidden))

	executeMerge(t, model.EntityTypeCharacter, src.ID, dst.ID, "same character")

	var links []model.CatalogCharacterTraitLink
	require.NoError(t, testDB.Where("character_id = ?", dst.ID).Find(&links).Error)
	require.Len(t, links, 1, "the loser's suppression reaches the survivor's upstream link")
	assert.Equal(t, regraded, links[0].TraitID)
	assert.EqualValues(t, 12, links[0].SourceID, "the curated twin wins over the survivor's upstream row")
	assert.EqualValues(t, 2, links[0].SpoilerLevel)

	var got model.CatalogCharacter
	require.NoError(t, testDB.First(&got, inst.ID).Error)
	require.NotNil(t, got.InstanceOf)
	assert.Equal(t, dst.ID, *got.InstanceOf, "instance_of follows the merge")
}

func TestLabelMergeFollowsCuratedAliasesAndRelationKeys(t *testing.T) {
	cleanTables(t)
	src := &model.CatalogLabel{DisplayName: "brand", Kind: model.LabelKindGameBrand}
	dst := &model.CatalogLabel{DisplayName: "Brand", Kind: model.LabelKindGameBrand}
	watcher := &model.CatalogLabel{DisplayName: "Parent", Kind: model.LabelKindPublisher}
	for _, l := range []*model.CatalogLabel{src, dst, watcher} {
		require.NoError(t, testDB.Create(l).Error)
	}
	curated, upstream := int16(12), int16(3)
	require.NoError(t, testDB.Create(&model.CatalogLabelAlias{LabelID: src.ID, Name: "ブランド", Lang: "ja", Kind: 1, SourceID: &curated}).Error)
	require.NoError(t, testDB.Create(&model.CatalogLabelAlias{LabelID: dst.ID, Name: "ブランド", Lang: "ja", Kind: 1, SourceID: &upstream}).Error)
	suppressRow(t, editspec.TypeLabel, src.ID, editspec.FieldLabelAliases, editspec.LabelAliasIdentity(1, "ja", "誤名"))
	relKey := editspec.LabelRelationIdentity(model.LabelRelationSubsidiary, src.ID)
	suppressRow(t, editspec.TypeLabel, watcher.ID, editspec.FieldLabelRelations, relKey)

	executeMerge(t, model.EntityTypeLabel, src.ID, dst.ID, "same brand")

	var alias model.CatalogLabelAlias
	require.NoError(t, testDB.Where("label_id = ? AND name = ?", dst.ID, "ブランド").First(&alias).Error)
	require.NotNil(t, alias.SourceID)
	assert.EqualValues(t, 12, *alias.SourceID, "the curated alias wins over the survivor's upstream twin")

	aliasKeys, err := editing.LoadSuppressedKeys(t.Context(), testDB, editspec.TypeLabel, dst.ID, editspec.FieldLabelAliases)
	require.NoError(t, err)
	assert.Equal(t, []string{editspec.LabelAliasIdentity(1, "ja", "誤名")}, aliasKeys, "the loser's own suppressions move to the survivor")

	relKeys, err := editing.LoadSuppressedKeys(t.Context(), testDB, editspec.TypeLabel, watcher.ID, editspec.FieldLabelRelations)
	require.NoError(t, err)
	assert.Equal(t, []string{editspec.LabelRelationIdentity(model.LabelRelationSubsidiary, dst.ID)}, relKeys,
		"another company's suppression naming the loser is re-keyed to the survivor")
}
