package service

import (
	"fmt"
	"slices"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/require"
)

func TestMergeCarriesWorkTagSuppressionsAndPurgesTheSurvivor(t *testing.T) {
	cleanTables(t)
	for _, table := range []string{"catalog_tag_source_map", "catalog_tag"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
	ctx := t.Context()
	const vndb, bangumi int16 = 2, 3

	tags := map[string]model.CatalogTag{}
	for _, name := range []string{"肛交", "NTR", "恋爱"} {
		tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent}
		require.NoError(t, testDB.Create(&tag).Error)
		tags[name] = tag
		for _, source := range []int16{vndb, bangumi} {
			require.NoError(t, testDB.Create(&model.CatalogTagSourceMap{
				SourceID: source, SourceName: name, TagID: tag.ID,
			}).Error)
		}
	}
	suppress := func(workID int64, tag string) {
		require.NoError(t, testDB.Create(&editing.SuppressedRow{
			EntityType: editspec.TypeWork, EntityID: workID, FieldKey: editspec.FieldWorkTagIDs,
			IdentityKey: editspec.WorkTagIdentity(tags[tag].ID),
		}).Error)
	}

	target := createWork(t, "生き残る")
	source := createWork(t, "消える")
	for _, row := range []model.CatalogWorkTag{
		{WorkID: target.ID, SourceID: vndb, Name: "肛交"},
		{WorkID: target.ID, SourceID: bangumi, Name: "恋爱"},
		{WorkID: source.ID, SourceID: bangumi, Name: "肛交"},
		{WorkID: source.ID, SourceID: vndb, Name: "NTR"},
		{WorkID: source.ID, SourceID: vndb, Name: "恋爱"},
	} {
		require.NoError(t, testDB.Create(&row).Error)
	}
	suppress(source.ID, "肛交")
	suppress(target.ID, "NTR")

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, source.ID, target.ID, 7, "tag suppression rehang")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	keys, err := editing.LoadSuppressedKeys(ctx, testDB, editspec.TypeWork, target.ID, editspec.FieldWorkTagIDs)
	require.NoError(t, err)
	want := []string{editspec.WorkTagIdentity(tags["肛交"].ID), editspec.WorkTagIdentity(tags["NTR"].ID)}
	slices.Sort(want)
	require.Equal(t, want, keys, "both works' suppressions land on the survivor")
	left, err := editing.LoadSuppressedKeys(ctx, testDB, editspec.TypeWork, source.ID, editspec.FieldWorkTagIDs)
	require.NoError(t, err)
	require.Empty(t, left)

	var rows []model.CatalogWorkTag
	require.NoError(t, testDB.Where("work_id IN ?", []int64{target.ID, source.ID}).
		Order("work_id, source_id, name").Find(&rows).Error)
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%d/%d:%s", r.WorkID, r.SourceID, r.Name))
	}
	require.Equal(t, []string{
		fmt.Sprintf("%d/%d:恋爱", target.ID, vndb),
		fmt.Sprintf("%d/%d:恋爱", target.ID, bangumi),
	}, got, "no row of a tag suppressed on the survivor may outlive the merge, whichever side brought it")
}
