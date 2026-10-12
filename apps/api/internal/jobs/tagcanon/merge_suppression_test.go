package tagcanon

import (
	"context"
	"fmt"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeCarriesWorkTagSuppressionsToTheWinner(t *testing.T) {
	cleanTagcanon(t)
	truncate := func() {
		require.NoError(t, testDB.Exec("TRUNCATE edit_suppressed_row RESTART IDENTITY").Error)
	}
	truncate()
	t.Cleanup(truncate)
	ctx := context.Background()
	bgm, vndb := srcID(t, sourceKeyBangumi), srcID(t, sourceKeyVNDB)

	winner := mkTag(t, "致郁", false)
	loser := mkTag(t, "致郁（鬱ゲー）", false)
	mkMap(t, bgm, "致郁", winner.ID)
	mkMap(t, vndb, "致郁（鬱ゲー）", loser.ID)

	var medium int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key='galgame'`).Scan(&medium).Error)
	suppressedLoser, suppressedWinner, suppressedBoth, untouched :=
		mkBodylessWork(t, medium), mkBodylessWork(t, medium), mkBodylessWork(t, medium), mkBodylessWork(t, medium)
	suppress := func(workID, tagID int64) {
		require.NoError(t, testDB.Create(&editing.SuppressedRow{
			EntityType: editspec.TypeWork, EntityID: workID, FieldKey: editspec.FieldWorkTagIDs,
			IdentityKey: editspec.WorkTagIdentity(tagID),
		}).Error)
	}
	suppress(suppressedLoser, loser.ID)
	mkWorkTag(t, suppressedLoser, "致郁", 1, bgm)
	suppress(suppressedWinner, winner.ID)
	mkWorkTag(t, suppressedWinner, "致郁（鬱ゲー）", 1, vndb)
	suppress(suppressedBoth, loser.ID)
	suppress(suppressedBoth, winner.ID)
	mkWorkTag(t, untouched, "致郁", 1, bgm)
	mkWorkTag(t, untouched, "致郁（鬱ゲー）", 1, vndb)

	path := writeMergeFile(t, mergeRec{FromID: loser.ID, From: "致郁（鬱ゲー）", IntoID: winner.ID, Into: "致郁"})
	st, err := Merge(ctx, MergeOpts{DSN: testDSN, Merges: path, Apply: true})
	require.NoError(t, err)
	assert.Zero(t, st.Errors)
	assert.Equal(t, 1, st.SuppressionsMoved)

	var rows []editing.SuppressedRow
	require.NoError(t, testDB.Order("entity_id").Find(&rows).Error)
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%d/%s/%s", r.EntityID, r.FieldKey, r.IdentityKey))
	}
	key := editspec.WorkTagIdentity(winner.ID)
	assert.Equal(t, []string{
		fmt.Sprintf("%d/%s/%s", suppressedLoser, editspec.FieldWorkTagIDs, key),
		fmt.Sprintf("%d/%s/%s", suppressedWinner, editspec.FieldWorkTagIDs, key),
		fmt.Sprintf("%d/%s/%s", suppressedBoth, editspec.FieldWorkTagIDs, key),
	}, got, "a suppression of the losing tag now names the winner")

	var tags []model.CatalogWorkTag
	require.NoError(t, testDB.Order("work_id, name").Find(&tags).Error)
	left := make([]string, 0, len(tags))
	for _, r := range tags {
		left = append(left, fmt.Sprintf("%d:%s", r.WorkID, r.Name))
	}
	assert.Equal(t, []string{
		fmt.Sprintf("%d:致郁", untouched),
		fmt.Sprintf("%d:致郁（鬱ゲー）", untouched),
	}, left, "rows that now resolve to a tag their work suppresses are gone")
}
