package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wikiSourceID(t *testing.T) int16 {
	t.Helper()
	var rows []struct {
		Key string `gorm:"column:key"`
		ID  int16  `gorm:"column:id"`
	}
	if err := testDB.Raw(`SELECT key, id FROM catalog_source WHERE key IN ?`, curatedSourceKeys).Scan(&rows).Error; err != nil {
		t.Fatalf("curated source lookup: %v", err)
	}
	byKey := make(map[string]int16, len(rows))
	for _, r := range rows {
		byKey[r.Key] = r.ID
	}
	id := curatedSourceID(byKey)
	if id == 0 {
		t.Fatalf("curated source absent (looked for %v)", curatedSourceKeys)
	}
	return id
}

func TestMergeKeepsWikiIdMapRefsExact(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	wikiSrc := wikiSourceID(t)

	target := &model.CatalogLabel{DisplayName: "Survivor Brand", Kind: model.LabelKindGameBrand}
	require.NoError(t, testDB.Create(target).Error)
	source := &model.CatalogLabel{DisplayName: "Merged Brand", Kind: model.LabelKindGameBrand}
	require.NoError(t, testDB.Create(source).Error)

	addExternalRef(t, model.EntityTypeLabel, target.ID, wikiSrc, "1001", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeLabel, source.ID, wikiSrc, "1002", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeLabel, target.ID, srcVNDB, "p111", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeLabel, source.ID, srcVNDB, "p222", model.LinkKindExact)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeLabel, source.ID, target.ID, 7, "same brand")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	type refRow struct {
		SourceID   int16
		ExternalID string
		LinkKind   int16
	}
	var refs []refRow
	require.NoError(t, testDB.Raw(`SELECT source_id, external_id, link_kind FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? ORDER BY source_id, external_id`,
		model.EntityTypeLabel, target.ID).Scan(&refs).Error)

	assert.Equal(t, []refRow{
		{srcVNDB, "p111", model.LinkKindExact},
		{srcVNDB, "p222", model.LinkKindProbable},
		{wikiSrc, "1001", model.LinkKindExact},
		{wikiSrc, "1002", model.LinkKindExact},
	}, refs, "both wiki oids must stay exact; only the loser's competing vndb exact demotes")
}

func TestMergeKeepsWikiGidRefsExactOnWorks(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	wikiSrc := wikiSourceID(t)

	target := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Survivor")
	source := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Duplicate")
	addExternalRef(t, model.EntityTypeWork, target.ID, wikiSrc, "5001", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, source.ID, wikiSrc, "5002", model.LinkKindExact)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, source.ID, target.ID, 7, "same game")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var kinds []int16
	require.NoError(t, testDB.Raw(`SELECT link_kind FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND source_id = ? ORDER BY external_id`,
		model.EntityTypeWork, target.ID, wikiSrc).Scan(&kinds).Error)
	assert.Equal(t, []int16{model.LinkKindExact, model.LinkKindExact}, kinds,
		"both wiki gids must stay exact so both old /galgame/<gid> URLs keep resolving")
}
