package getchuattach

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func mkVNDBEdition(t *testing.T, workID int64, vndbID, platform, srcPlatform string, y, m, d int16) int64 {
	t.Helper()
	rel := model.CatalogRelease{
		WorkID: workID, Kind: model.ReleaseKindDefault, ReleasedY: &y, ReleasedM: &m, ReleasedD: &d,
		Extra: datatypes.JSON([]byte(`{}`)), FieldProvenance: datatypes.JSON([]byte(`{}`)),
	}
	if platform != "" {
		rel.Platform = &platform
	}
	require.NoError(t, testDB.Create(&rel).Error)
	insertVNDBRelease(t, vndbID, 0)
	mkRef(t, model.EntityTypeRelease, rel.ID, sourceID(t, "vndb"), vndbID, model.LinkKindExact)
	if srcPlatform != "" {
		require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_platforms (id, platform) VALUES (?, ?)`, vndbID, srcPlatform).Error)
	}
	return rel.ID
}

func mkParallel(t *testing.T, workID int64, getchuID string, y, m, d int16) int64 {
	t.Helper()
	id := mkReleaseKind(t, workID, model.ReleaseKindPhysical, &y, &m, &d)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: id, SourceID: sourceID(t, "getchu"),
		ExternalID: getchuID, LinkKind: model.LinkKindExact, MatchedBy: ruleTitleDate,
	}).Error)
	return id
}

func linkVNDBGetchu(t *testing.T, vndbID, getchuID string, link int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.extlinks (id, site, value) VALUES (?, 'getchu', ?)`, link, getchuID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_extlinks (id, link) VALUES (?, ?)`, vndbID, link).Error)
}

func liveRelease(t *testing.T, id int64) bool {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_release WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n).Error)
	return n == 1
}

func TestGetchuSKUsJoinTheirPlatformEditions(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "君色パトラIF")
	win := mkVNDBEdition(t, w, "r140215", "", "win", 2026, 11, 26)
	swi := mkVNDBEdition(t, w, "r161876", "swi", "swi", 2026, 11, 26)
	ps5 := mkVNDBEdition(t, w, "r161877", "ps5", "ps5", 2026, 11, 26)
	insertFetched(t, "1373848", "君色パトラIF 完全生産限定版 PlayStation 5版", "エンターグラム", "2026/11/26")
	insertFetched(t, "1373849", "君色パトラIF 完全生産限定版 Nintendo Switch版", "エンターグラム", "2026/11/26")
	insertFetched(t, "1373850", "君色パトラIF 完全生産限定版", "エンターグラム", "2026/11/26")

	before := releaseCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 3, st.Attached)
	assert.Equal(t, 3, st.EditionAnchored)
	assert.Equal(t, before, releaseCount(t), "each SKU joins its edition instead of founding a row")
	assert.Equal(t, ps5, attachedRelease(t, "1373848").ID)
	assert.Equal(t, swi, attachedRelease(t, "1373849").ID)
	assert.Equal(t, win, attachedRelease(t, "1373850").ID)
}

func TestConsoleSKUNeverJoinsAPCEdition(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Console Only Title")
	mkVNDBEdition(t, w, "r9101", "win", "win", 2001, 1, 1)
	insertFetched(t, "910", "Console Only Title PlayStation 5版", "", "2001/01/01")

	before := releaseCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	assert.Zero(t, st.EditionAnchored)
	assert.Equal(t, before+1, releaseCount(t), "an edition the catalog lacks keeps its own row")
}

func TestFoldMovesTheParallelRowOntoItsEdition(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Fold Host Title")
	edition := mkVNDBEdition(t, w, "r9201", "win", "win", 2001, 1, 1)
	parallel := mkParallel(t, w, "920", 2001, 1, 1)
	insertFetched(t, "920", "Fold Host Title 初回限定版", "", "2001/01/01")

	dry := runLane(t, false, "")
	assert.Equal(t, 1, dry.Folds)
	assert.Zero(t, dry.RowsRetired)
	assert.Equal(t, parallel, attachedRelease(t, "920").ID, "a dry run moves nothing")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Folds)
	assert.Equal(t, 1, st.RowsRetired)
	refs := getchuRefs(t, "920")
	require.Len(t, refs, 1)
	assert.Equal(t, edition, refs[0].EntityID)
	assert.Equal(t, ruleTitleDate, refs[0].MatchedBy)
	assert.False(t, liveRelease(t, parallel))
	assert.True(t, liveRelease(t, edition))
	var rev model.CatalogRevision
	require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ?", model.EntityTypeRelease, parallel).
		Order("revision DESC").First(&rev).Error)
	assert.Equal(t, model.RevisionActionDeleted, rev.Action)
	assert.Contains(t, rev.Note, "folded into release")

	again := runLane(t, true, "")
	assert.Zero(t, again.Folds)
}

func TestFoldFollowsAVNDBExtlinkAcrossADelay(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Delayed Host Title")
	edition := mkVNDBEdition(t, w, "r9301", "win", "win", 2027, 1, 28)
	parallel := mkParallel(t, w, "930", 2026, 11, 26)
	insertFetched(t, "930", "Delayed Host Title", "", "2026/11/26")
	linkVNDBGetchu(t, "r9301", "930", 1)

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Folds)
	assert.Equal(t, edition, attachedRelease(t, "930").ID)
	assert.False(t, liveRelease(t, parallel))
}

func TestFoldKeepsAParallelRowWithoutEvidence(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Unproven Host Title")
	mkVNDBEdition(t, w, "r9401", "win", "win", 2027, 1, 28)
	parallel := mkParallel(t, w, "940", 2026, 11, 26)
	insertFetched(t, "940", "Unproven Host Title", "", "2026/11/26")

	st := runLane(t, true, "")
	assert.Zero(t, st.Folds)
	assert.Equal(t, 1, st.ParallelKept)
	assert.Equal(t, parallel, attachedRelease(t, "940").ID)
}

func TestFoldLeavesAHumanEditedRow(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Edited Host Title")
	mkVNDBEdition(t, w, "r9501", "win", "win", 2001, 1, 1)
	parallel := mkParallel(t, w, "950", 2001, 1, 1)
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET field_provenance = '{"released":"human:1"}' WHERE id = ?`, parallel).Error)
	insertFetched(t, "950", "Edited Host Title", "", "2001/01/01")

	st := runLane(t, true, "")
	assert.Zero(t, st.Folds)
	assert.Equal(t, parallel, attachedRelease(t, "950").ID)
	assert.True(t, liveRelease(t, parallel))
}

func TestParallelCheckCoversEveryReleaseReference(t *testing.T) {
	requireDB(t)
	var tables []string
	require.NoError(t, testDB.Raw(`SELECT conrelid::regclass::text FROM pg_constraint
		WHERE confrelid = 'catalog_release'::regclass AND contype = 'f' ORDER BY 1`).Scan(&tables).Error)
	assert.Equal(t, []string{"catalog_credit", "catalog_release_label", "catalog_walkthrough"}, tables,
		"a new table pointing at catalog_release must join parallelRowSQL, or a fold retires a row it still holds")
}

func TestAJoinedEGRowStaysACounterpart(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "EG Minted Host")
	egRow := mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertFetched(t, "960", "EG Minted Host 初回限定版", "", "2001/01/01")
	first := runLane(t, true, "")
	assert.Equal(t, 1, first.EditionAnchored)
	assert.Equal(t, egRow, attachedRelease(t, "960").ID)

	insertFetched(t, "961", "EG Minted Host 通常版", "", "2001/01/01")
	before := releaseCount(t)
	second := runLane(t, true, "")
	assert.Equal(t, 1, second.EditionAnchored, "a ref-less row that took a Getchu ref still stands for its edition")
	assert.Zero(t, second.Folds)
	assert.Zero(t, second.ParallelKept)
	assert.Equal(t, before, releaseCount(t))
	assert.Equal(t, egRow, attachedRelease(t, "961").ID)
	assert.True(t, liveRelease(t, egRow))
}
