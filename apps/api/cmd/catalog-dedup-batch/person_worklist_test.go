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

func TestPersonWorklistMergesAndTouchesNoWork(t *testing.T) {
	for _, tbl := range []string{
		"catalog_merge_proposal", "catalog_redirect", "catalog_external_ref",
		"catalog_credit", "catalog_credit_name", "catalog_person", "catalog_work",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	ctx := context.Background()
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve)

	host := model.CatalogPerson{DisplayName: "北都南"}
	loser := model.CatalogPerson{DisplayName: "ひと美"}
	require.NoError(t, testDB.Create(&host).Error)
	require.NoError(t, testDB.Create(&loser).Error)

	kept := model.CatalogCreditName{Name: "北都南", PersonID: &host.ID}
	moved := model.CatalogCreditName{Name: "ひと美", PersonID: &loser.ID}
	require.NoError(t, testDB.Create(&kept).Error)
	require.NoError(t, testDB.Create(&moved).Error)

	for _, ref := range []model.CatalogExternalRef{
		{EntityType: model.EntityTypePerson, EntityID: host.ID, SourceID: 2, ExternalID: "s1", LinkKind: model.LinkKindExact},
		{EntityType: model.EntityTypePerson, EntityID: loser.ID, SourceID: 2, ExternalID: "s2", LinkKind: model.LinkKindExact},
		{EntityType: model.EntityTypePerson, EntityID: loser.ID, SourceID: 3, ExternalID: "b9", LinkKind: model.LinkKindExact},
	} {
		require.NoError(t, testDB.Create(&ref).Error)
	}

	work := model.CatalogWork{DisplayName: "W", MediumID: 1}
	require.NoError(t, testDB.Create(&work).Error)
	var beforeWork model.CatalogWork
	require.NoError(t, testDB.First(&beforeWork, work.ID).Error)

	path := writeWorklist(t, `{"class":"person","survivor":`+strconv.FormatInt(host.ID, 10)+
		`,"sources":[`+strconv.FormatInt(loser.ID, 10)+`]}`)
	const tag156 = "rule:catalog-dedup step-156"

	require.NoError(t, runPropose(ctx, testDB, io.Discard, merge, 1, "", path, tag156, 0, true))
	var opened []model.CatalogMergeProposal
	require.NoError(t, testDB.Where("note LIKE ?", "%"+tag156+"%").Find(&opened).Error)
	require.Len(t, opened, 1)
	assert.Equal(t, model.EntityTypePerson, opened[0].EntityType,
		"the person class must open a PERSON-typed proposal, not the character default")

	require.NoError(t, runExecute(ctx, testDB, io.Discard, merge, resolve, 1, waveTag154, 0, true))
	require.NoError(t, testDB.First(&opened[0], opened[0].ID).Error)
	assert.Equal(t, model.ProposalStatusApproved, opened[0].Status)

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour'`).Error)
	require.NoError(t, runExecute(ctx, testDB, io.Discard, merge, resolve, 1, tag156, 0, true))

	var executed model.CatalogMergeProposal
	require.NoError(t, testDB.First(&executed, opened[0].ID).Error)
	assert.Equal(t, model.ProposalStatusExecuted, executed.Status)

	var names []model.CatalogCreditName
	require.NoError(t, testDB.Order("id").Find(&names).Error)
	require.Len(t, names, 2)
	for _, n := range names {
		require.NotNil(t, n.PersonID)
		assert.Equal(t, host.ID, *n.PersonID, "every credit name must hang off the survivor")
	}

	var refs []model.CatalogExternalRef
	require.NoError(t, testDB.Where("entity_type = ?", model.EntityTypePerson).
		Order("source_id, external_id").Find(&refs).Error)
	require.Len(t, refs, 3)
	for _, r := range refs {
		assert.Equal(t, host.ID, r.EntityID, "every anchor must hang off the survivor")
		want := model.LinkKindExact
		if r.ExternalID == "s2" {
			want = model.LinkKindProbable
		}
		assert.Equal(t, want, r.LinkKind,
			"the survivor keeps its own vndb staff id exact; the loser's competing one demotes to probable; the lone bangumi anchor stays exact")
	}

	var retired model.CatalogPerson
	require.NoError(t, testDB.Unscoped().First(&retired, loser.ID).Error)
	assert.NotNil(t, retired.DeletedAt, "the loser person is soft-deleted")
	got, moved2, err := resolve.Resolve(ctx, model.EntityTypePerson, loser.ID)
	require.NoError(t, err)
	assert.True(t, moved2)
	assert.Equal(t, host.ID, got)

	var afterWork model.CatalogWork
	require.NoError(t, testDB.First(&afterWork, work.ID).Error)
	assert.Equal(t, beforeWork.UpdatedAt, afterWork.UpdatedAt,
		"a person merge must touch no work: person never reaches a work read face")
}
