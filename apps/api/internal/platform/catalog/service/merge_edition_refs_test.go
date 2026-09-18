package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type editionRefRow struct {
	ExternalID string
	LinkKind   int16
}

func executeWorkMerge(t *testing.T, src, dst int64) {
	t.Helper()
	p, err := testMerge.ProposeMerge(t.Context(), model.EntityTypeWork, src, dst, 7, "edition split")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil))
}

func workRefs(t *testing.T, workID int64, sourceID int16) []editionRefRow {
	t.Helper()
	var refs []editionRefRow
	require.NoError(t, testDB.Raw(`SELECT external_id, link_kind FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND source_id = ? ORDER BY external_id`,
		model.EntityTypeWork, workID, sourceID).Scan(&refs).Error)
	return refs
}

func TestMergeKeepsTargetsEGPrimaryAndRelatesTheOther(t *testing.T) {
	cleanTables(t)
	target := createWork(t, "恋姫†無双")
	source := createWork(t, "恋姫†無双 DL")
	addExternalRef(t, model.EntityTypeWork, target.ID, model.SourceErogameScape, "211785", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, source.ID, model.SourceErogameScape, "1824", model.LinkKindExact)

	executeWorkMerge(t, source.ID, target.ID)

	assert.Equal(t, []editionRefRow{
		{"1824", model.LinkKindRelated},
		{"211785", model.LinkKindExact},
	}, workRefs(t, target.ID, model.SourceErogameScape))
}

func TestMergeKeepsLowestEGExactWhenTargetHadNone(t *testing.T) {
	cleanTables(t)
	target := createWork(t, "加奈～いもうと～")
	source := createWork(t, "加奈 ～いもうと～　[7対応版]")
	addExternalRef(t, model.EntityTypeWork, source.ID, model.SourceErogameScape, "211785", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, source.ID, model.SourceErogameScape, "1824", model.LinkKindExact)

	executeWorkMerge(t, source.ID, target.ID)

	assert.Equal(t, []editionRefRow{
		{"1824", model.LinkKindExact},
		{"211785", model.LinkKindRelated},
	}, workRefs(t, target.ID, model.SourceErogameScape))
}

func TestMergeStillDemotesConflictingVNDBExacts(t *testing.T) {
	cleanTables(t)
	target := createWork(t, "Faith")
	source := createWork(t, "Faith remake")
	addExternalRef(t, model.EntityTypeWork, target.ID, srcVNDB, "v1", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, source.ID, srcVNDB, "v2", model.LinkKindExact)

	executeWorkMerge(t, source.ID, target.ID)

	assert.Equal(t, []editionRefRow{
		{"v1", model.LinkKindProbable},
		{"v2", model.LinkKindProbable},
	}, workRefs(t, target.ID, srcVNDB))
}

func TestMergeBangumiEditionIdsBecomeRelated(t *testing.T) {
	cleanTables(t)
	target := createWork(t, "円環の柩")
	source := createWork(t, "円環の柩 HD")
	addExternalRef(t, model.EntityTypeWork, target.ID, model.SourceBangumi, "100", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, source.ID, model.SourceBangumi, "200", model.LinkKindExact)

	executeWorkMerge(t, source.ID, target.ID)

	assert.Equal(t, []editionRefRow{
		{"100", model.LinkKindExact},
		{"200", model.LinkKindRelated},
	}, workRefs(t, target.ID, model.SourceBangumi))
}
