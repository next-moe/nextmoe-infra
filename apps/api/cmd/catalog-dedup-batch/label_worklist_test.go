package main

import (
	"context"
	"io"
	"strconv"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLabelClassMapsToLabelEntity(t *testing.T) {
	groups, err := loadWorklist(writeWorklist(t,
		`{"class":"label","survivor":11871,"sources":[12775],"evidence":{"anchors":"dlsite VG02008 vs VG01282"}}`))
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, classLabel, groups[0].class)
	assert.Equal(t, int64(11871), groups[0].survivor)
	assert.Equal(t, []int64{12775}, groups[0].sources)
	assert.Equal(t, model.EntityTypeLabel, entityTypeOf(classLabel))
}

func TestLabelWorklistMergesBrandFacets(t *testing.T) {
	for _, tbl := range []string{
		"catalog_merge_proposal", "catalog_redirect", "catalog_external_ref",
		"catalog_work_label", "catalog_label_alias", "catalog_label_intro",
		"catalog_label", "catalog_work",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	ctx := context.Background()
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve)

	host := model.CatalogLabel{DisplayName: "アーカムプロダクツ", Lang: "ja", Kind: model.LabelKindPublisher}
	loser := model.CatalogLabel{DisplayName: "アーカムプロダクツ", Lang: "ja", Kind: model.LabelKindPublisher}
	require.NoError(t, testDB.Create(&host).Error)
	require.NoError(t, testDB.Create(&loser).Error)

	hostWork := model.CatalogWork{DisplayName: "H", MediumID: 1}
	loserWork := model.CatalogWork{DisplayName: "L", MediumID: 1}
	require.NoError(t, testDB.Create(&hostWork).Error)
	require.NoError(t, testDB.Create(&loserWork).Error)
	for _, e := range []model.CatalogWorkLabel{
		{WorkID: hostWork.ID, LabelID: host.ID, Kind: model.WorkLabelKindDeveloper},
		{WorkID: loserWork.ID, LabelID: loser.ID, Kind: model.WorkLabelKindDeveloper},
	} {
		require.NoError(t, testDB.Create(&e).Error)
	}
	require.NoError(t, testDB.Create(&model.CatalogLabelAlias{
		LabelID: loser.ID, Name: "チーム暗黒媒体", Kind: model.AliasKindSpellingVariant}).Error)

	for _, ref := range []model.CatalogExternalRef{
		{EntityType: model.EntityTypeLabel, EntityID: host.ID, SourceID: 4, ExternalID: "VG02008", LinkKind: model.LinkKindExact},
		{EntityType: model.EntityTypeLabel, EntityID: loser.ID, SourceID: 4, ExternalID: "VG01282", LinkKind: model.LinkKindExact},
	} {
		require.NoError(t, testDB.Create(&ref).Error)
	}

	var beforeWork model.CatalogWork
	require.NoError(t, testDB.First(&beforeWork, loserWork.ID).Error)

	path := writeWorklist(t, `{"class":"label","survivor":`+strconv.FormatInt(host.ID, 10)+
		`,"sources":[`+strconv.FormatInt(loser.ID, 10)+`]}`)
	const tag175 = "rule:catalog-dedup step-175"

	require.NoError(t, runPropose(ctx, testDB, io.Discard, merge, 1, "", path, tag175, 0, true))
	var opened []model.CatalogMergeProposal
	require.NoError(t, testDB.Where("note LIKE ?", "%"+tag175+"%").Find(&opened).Error)
	require.Len(t, opened, 1)
	assert.Equal(t, model.EntityTypeLabel, opened[0].EntityType,
		"the label class must open a LABEL-typed proposal, not the character default")

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour'`).Error)
	require.NoError(t, runExecute(ctx, testDB, io.Discard, merge, resolve, 1, tag175, 0, true))

	var executed model.CatalogMergeProposal
	require.NoError(t, testDB.First(&executed, opened[0].ID).Error)
	assert.Equal(t, model.ProposalStatusExecuted, executed.Status)

	var edges []model.CatalogWorkLabel
	require.NoError(t, testDB.Order("work_id").Find(&edges).Error)
	require.Len(t, edges, 2)
	for _, e := range edges {
		assert.Equal(t, host.ID, e.LabelID, "every brand edge must hang off the survivor")
	}

	var aliases []model.CatalogLabelAlias
	require.NoError(t, testDB.Find(&aliases).Error)
	require.Len(t, aliases, 1)
	assert.Equal(t, host.ID, aliases[0].LabelID, "the loser's alias must move to the survivor")

	var refs []model.CatalogExternalRef
	require.NoError(t, testDB.Where("entity_type = ?", model.EntityTypeLabel).Order("external_id").Find(&refs).Error)
	require.Len(t, refs, 2)
	for _, r := range refs {
		assert.Equal(t, host.ID, r.EntityID)
		want := model.LinkKindExact
		if r.ExternalID == "VG01282" {
			want = model.LinkKindProbable
		}
		assert.Equal(t, want, r.LinkKind,
			"the survivor keeps its own dlsite maker id exact; only the loser's competing one demotes to probable")
	}

	var retired model.CatalogLabel
	require.NoError(t, testDB.Unscoped().First(&retired, loser.ID).Error)
	assert.NotNil(t, retired.DeletedAt, "the loser label is soft-deleted")
	got, moved, err := resolve.Resolve(ctx, model.EntityTypeLabel, loser.ID)
	require.NoError(t, err)
	assert.True(t, moved)
	assert.Equal(t, host.ID, got)

	var afterWork model.CatalogWork
	require.NoError(t, testDB.First(&afterWork, loserWork.ID).Error)
	assert.True(t, afterWork.UpdatedAt.After(beforeWork.UpdatedAt),
		"a label merge rewrites the host work's read face, so the work must be touched")
}
