package bgmworkmeta

import (
	"context"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuppressedMetaTagIsNeverWritten(t *testing.T) {
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

	wa2 := mkWork(t, reg.galgameMedium, "WHITE ALBUM2", nil)
	mkSubject(t, 501, `["R18", "PC"]`, "")
	mkAnchor(t, wa2, "501", reg.bangumiSource, model.LinkKindExact, "rule:bgm-title-year")
	other := mkWork(t, reg.galgameMedium, "other", nil)
	mkSubject(t, 502, `["R18", "PC"]`, "")
	mkAnchor(t, other, "502", reg.bangumiSource, model.LinkKindExact, "rule:bgm-title-year")

	tag := model.CatalogTag{Name: "R18", Tier: model.TagTierCore, Kind: model.TagKindMeta}
	require.NoError(t, testDB.Create(&tag).Error)
	require.NoError(t, testDB.Create(&model.CatalogTagSourceMap{SourceID: reg.bangumiSource, SourceName: "R18", TagID: tag.ID}).Error)
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: wa2, FieldKey: editspec.FieldWorkTagIDs,
		IdentityKey: editspec.WorkTagIdentity(tag.ID),
	}).Error)

	st, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.MetaSuppressed)
	assert.Equal(t, 3, st.MetaWritten)
	assert.Zero(t, st.Errors)
	assert.EqualValues(t, 0, tagCount(t, "WHERE work_id = ? AND name = ?", wa2, "R18"), "the suppressed pair is skipped")
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ? AND name = ?", wa2, "PC"), "the work's other tags are written")
	assert.EqualValues(t, 2, tagCount(t, "WHERE work_id = ?", other), "the same tag on another work is written")
}
