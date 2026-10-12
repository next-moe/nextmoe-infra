package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTrait(t *testing.T, tid, name string) *model.CatalogCharacterTrait {
	t.Helper()
	tr := &model.CatalogCharacterTrait{VndbTID: tid, Name: name, GroupTID: "g"}
	require.NoError(t, testDB.Create(tr).Error)
	return tr
}

func TestMergeCharacterCarriesIntrosAndTraits(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()

	target := createCharacter(t, "夏侯淵 妙才")
	source := createCharacter(t, "夏侯淵妙才")

	mkIntro := func(cid int64, lang string, src int16, body string) {
		require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
			CharacterID: cid, Lang: lang, SourceID: src, Intro: body}).Error)
	}
	mkIntro(target.ID, "ja", 2, "target ja/2")
	mkIntro(source.ID, "ja", 2, "source ja/2 — loses to the survivor")
	mkIntro(source.ID, "ja", 3, "source ja/3 — moves")
	mkIntro(source.ID, "zh-Hans", 2, "source zh/2 — moves")

	shared := createTrait(t, "g1", "Shared")
	only := createTrait(t, "g2", "SourceOnly")
	mkLink := func(cid, tid int64, spoiler int16) {
		require.NoError(t, testDB.Create(&model.CatalogCharacterTraitLink{
			CharacterID: cid, TraitID: tid, SpoilerLevel: spoiler}).Error)
	}
	mkLink(target.ID, shared.ID, 0)
	mkLink(source.ID, shared.ID, 2)
	mkLink(source.ID, only.ID, 1)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeCharacter, source.ID, target.ID, 7, "wave 154")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var intros []model.CatalogCharacterIntro
	require.NoError(t, testDB.Where("character_id = ?", target.ID).
		Order("lang, source_id").Find(&intros).Error)
	require.Len(t, intros, 2, "the survivor's ja keeps out every loser ja; zh-Hans is new and moves")
	assert.Equal(t, "target ja/2", intros[0].Intro)
	assert.Equal(t, "zh-Hans", intros[1].Lang)
	var archived int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_merge_archive
		WHERE proposal_id = ? AND table_name = 'catalog_character_intro'`, p.ID).Scan(&archived).Error)
	assert.EqualValues(t, 2, archived, "both loser ja intros are archived, not lost")

	var links []model.CatalogCharacterTraitLink
	require.NoError(t, testDB.Where("character_id = ?", target.ID).
		Order("trait_id").Find(&links).Error)
	require.Len(t, links, 2, "the non-colliding source trait link moves onto the host")
	assert.EqualValues(t, 0, links[0].SpoilerLevel, "the colliding key keeps the survivor's row")
	assert.Equal(t, only.ID, links[1].TraitID)

	var strandedIntros, strandedLinks int64
	require.NoError(t, testDB.Model(&model.CatalogCharacterIntro{}).
		Where("character_id = ?", source.ID).Count(&strandedIntros).Error)
	require.NoError(t, testDB.Model(&model.CatalogCharacterTraitLink{}).
		Where("character_id = ?", source.ID).Count(&strandedLinks).Error)
	assert.Zero(t, strandedIntros)
	assert.Zero(t, strandedLinks)
}
