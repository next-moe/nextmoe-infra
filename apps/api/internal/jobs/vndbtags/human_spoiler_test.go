package vndbtags

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateKeepsASpoilerAPersonSet(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "human-spoiler")
	id := mkWorkTag(t, w, "zz-human", vndb, 1, 2, false)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work_tag
		SET field_provenance = '{"spoiler":[{"source":"curated","at":"2026-10-20T00:00:00Z"}]}'
		WHERE id = ?`, id).Error)

	n, _, err := applyUpdate(testDB, w, vndb, Update{
		ID: id, Name: "zz-human", OldSpoiler: 2, NewSpoiler: 0, OldCount: 1, NewCount: 5,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	rows := loadWorkTags(t, w, vndb)
	require.Len(t, rows, 1)
	assert.EqualValues(t, 2, rows[0].Spoiler, "a human spoiler outlives the refresh")
	assert.Equal(t, 5, rows[0].Count, "the count still refreshes")
}
