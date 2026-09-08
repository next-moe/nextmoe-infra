package main

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

// catalog identifies only 29% of its live works by a vndb anchor (65,058 of
// 225,238), so a moyu page can be perfectly well known to catalog and still be
// invisible to the vndb-only route. On 2026-09-08 four such pages were mapped by
// hand from bangumi / erogamescape / dlsite anchors into patch.catalog_work_id
// and the lane read none of them: 34 favourites stranded with the answer already
// sitting in the row.
//
// The fallback is deliberately narrow — it fires only when the vndb route came
// back empty — and it still goes through resolve(), so a hand-written id naming
// a merged work is followed rather than written dead.
func TestMoyuFallsBackToTheMirrorColumnOnlyWhenVndbCannotAnswer(t *testing.T) {
	fx := seedFixture(t)
	require.NoError(t, testDB.Exec(`
		INSERT INTO patch (id, vndb_id, catalog_work_id) VALUES
			(1, 'pending-1', ?),
			(2, 'vtest-absent', ?),
			(3, 'vtest1', ?),
			(4, 'pending-4', NULL),
			(5, 'pending-5', ?)`,
		fx.live, fx.live, fx.merged, fx.merged).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO user_patch_favorite_relation (id, user_id, galgame_id, created, updated)
		VALUES (1, 21, 1, ?, ?), (2, 22, 2, ?, ?), (3, 23, 3, ?, ?),
		       (4, 24, 4, ?, ?), (5, 25, 5, ?, ?)`,
		at(1), at(1), at(1), at(1), at(1), at(1), at(1), at(1), at(1), at(1)).Error)

	imp := loadedImporter(t, true)
	c, err := imp.importMoyu()
	require.NoError(t, err)

	require.Equal(t, 4, c.ItemsInserted,
		"patches 1, 2, 3 and 5 all resolve; only patch 4 has no answer anywhere")
	require.Equal(t, 1, c.SkippedPending,
		"a pending- placeholder with an empty mirror column is still skipped")
	require.Zero(t, c.SkippedNoAnchor)

	// Patch 3's own vndb id resolves, so the mirror column must not be consulted:
	// were it consulted, this favourite would land on the merged work instead.
	require.Equal(t, fx.live, soleWorkOf(t, 23), "the vndb route wins whenever it can answer")

	// Patch 5's mirror column names a merged work, so the fallback has to follow
	// the redirect rather than write a dead id.
	require.Equal(t, fx.survivor, soleWorkOf(t, 25), "the fallback still follows a merge")
}

func soleWorkOf(t *testing.T, uid int64) int64 {
	t.Helper()
	var items []model.CatalogUserFolderItem
	require.NoError(t, testDB.
		Joins("JOIN catalog_user_folder f ON f.id = catalog_user_folder_item.folder_id").
		Where("f.owner_uid = ?", uid).Find(&items).Error)
	require.Len(t, items, 1)
	return items[0].WorkID
}
