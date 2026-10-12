package vndbtags

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetTagSuppression(t *testing.T) {
	t.Helper()
	truncate := func() {
		for _, tbl := range []string{"edit_suppressed_row", "catalog_tag_source_map", "catalog_tag"} {
			require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
		}
	}
	truncate()
	t.Cleanup(truncate)
}

func suppressTag(t *testing.T, workID int64, source int16, sourceName string) {
	t.Helper()
	tag := model.CatalogTag{Name: sourceName, Tier: model.TagTierCore, Kind: model.TagKindContent}
	require.NoError(t, testDB.Where("name = ?", sourceName).FirstOrCreate(&tag).Error)
	require.NoError(t, testDB.Where("source_id = ? AND source_name = ?", source, sourceName).
		FirstOrCreate(&model.CatalogTagSourceMap{SourceID: source, SourceName: sourceName, TagID: tag.ID}).Error)
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: workID, FieldKey: editspec.FieldWorkTagIDs,
		IdentityKey: editspec.WorkTagIdentity(tag.ID),
	}).Error)
}

func tagNames(t *testing.T, workID int64, src int16) []string {
	t.Helper()
	var names []string
	for _, row := range loadWorkTags(t, workID, src) {
		names = append(names, row.Name)
	}
	return names
}

func TestSuppressedTagIsNeverWritten(t *testing.T) {
	requireDB(t)
	resetTagSuppression(t)
	vndb := sourceID(t, "vndb")

	mkSrcTag(t, "g-anal", "ero", "Anal Sex", "", 0)
	mkSrcTag(t, "g-rom", "cont", "zz-romance", "", 0)
	mkVN(t, "v-wa2")
	mkVN(t, "v-other")
	wa2 := mkWork(t, "WHITE ALBUM2")
	mkAnchor(t, wa2, "v-wa2", model.LinkKindExact)
	other := mkWork(t, "other")
	mkAnchor(t, other, "v-other", model.LinkKindExact)
	for _, vid := range []string{"v-wa2", "v-other"} {
		mkKeptVotes(t, "g-anal", vid, 1, 0)
		mkKeptVotes(t, "g-rom", vid, 2, 0)
	}
	suppressTag(t, wa2, vndb, "肛交")

	dry := runJob(t, false, "")
	assert.Equal(t, 1, dry.TagsSuppressed)
	assert.Equal(t, 3, dry.TagsInserted)

	st := runJob(t, true, "")
	assert.Equal(t, 1, st.TagsSuppressed)
	assert.Equal(t, 3, st.TagsInserted)
	assert.Zero(t, st.Errors)
	assert.Equal(t, []string{"zz-romance"}, tagNames(t, wa2, vndb), "the suppressed pair is skipped, the work's other tags are written")
	assert.Equal(t, []string{"zz-romance", "肛交"}, tagNames(t, other, vndb), "the same tag on another work is written")

	mkWorkTag(t, wa2, "肛交", vndb, 1, 0, true)
	again := runJob(t, true, "")
	assert.Equal(t, 1, again.TagsDeleted, "a suppressed row that got in anyway is removed by the next sync")
	assert.Zero(t, again.TagsInserted)
	assert.Equal(t, []string{"zz-romance"}, tagNames(t, wa2, vndb))
}
