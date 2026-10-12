package dlsitegenres

import (
	"context"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuppressedGenreIsNeverWritten(t *testing.T) {
	clean(t)
	truncate := func() {
		for _, tbl := range []string{"edit_suppressed_row", "catalog_tag_source_map", "catalog_tag"} {
			require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
		}
	}
	truncate()
	t.Cleanup(truncate)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	mkTaxonomy(t, 101, "zh_CN", "肛交")
	mkTaxonomy(t, 102, "zh_CN", "纯爱")
	wa2 := mkWork(t, reg.galgameMedium, "WHITE ALBUM2", nil)
	mkReleaseAnchor(t, wa2, "RJ300001", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ300001", `[{"id":101},{"id":102}]`)
	other := mkWork(t, reg.galgameMedium, "other", nil)
	mkReleaseAnchor(t, other, "RJ300002", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ300002", `[{"id":101},{"id":102}]`)

	tag := model.CatalogTag{Name: "肛交", Tier: model.TagTierCore, Kind: model.TagKindContent}
	require.NoError(t, testDB.Create(&tag).Error)
	require.NoError(t, testDB.Create(&model.CatalogTagSourceMap{SourceID: reg.dlsiteSource, SourceName: "肛交", TagID: tag.ID}).Error)
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: wa2, FieldKey: editspec.FieldWorkTagIDs,
		IdentityKey: editspec.WorkTagIdentity(tag.ID),
	}).Error)

	st, err := Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 1, st.Suppressed)
	assert.Equal(t, 3, st.Written)
	assert.Zero(t, st.Errors)
	assert.EqualValues(t, 0, tagCount(t, "WHERE work_id = ? AND name = ?", wa2, "肛交"), "the suppressed pair is skipped")
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ? AND name = ?", wa2, "纯爱"), "the work's other tags are written")
	assert.EqualValues(t, 2, tagCount(t, "WHERE work_id = ?", other), "the same tag on another work is written")
}
