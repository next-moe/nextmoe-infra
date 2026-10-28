package migrate

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uploadedFixture(t *testing.T) {
	t.Helper()
	coverFixture(t)
	for id, key := range map[int16]string{model.SourceCurated: "curated", model.SourceUpscale: "upscale"} {
		require.NoError(t, testDB.Exec(`
			INSERT INTO catalog_source (id, key, trust_tier) VALUES (?, ?, 0)
			ON CONFLICT (id) DO NOTHING`, id, key).Error)
	}
}

func TestCoverArtGradeIgnoresUploadsOnceOfficialArtExists(t *testing.T) {
	uploadedFixture(t)
	w := newWork(t, "手打ちの検閲版")
	legacy := addCover(t, w, model.SourceCurated, "", strings.Repeat("c", 64), model.SexualSafe)
	assert.False(t, gradeOf(t, w), "an upload is all the work has, and it is safe")

	addCover(t, w, srcDLsite, "main", strings.Repeat("d", 64), model.SexualExplicit)
	assert.True(t, gradeOf(t, w), "the election shows the explicit official cover, not the safe upload")

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work_cover SET portrait_pinned = true WHERE id = ?`, legacy).Error)
	assert.False(t, gradeOf(t, w), "an editor's pin keeps the safe upload electable")

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work_cover SET portrait_pinned = false WHERE id = ?`, legacy).Error)
	require.NoError(t, testDB.Exec(
		`DELETE FROM catalog_work_cover WHERE work_id = ? AND source_id = ?`, w, srcDLsite).Error)
	assert.False(t, gradeOf(t, w), "with the official art gone the upload counts again")
}

func TestCoverArtGradeIgnoresMachinePinnedUpscales(t *testing.T) {
	uploadedFixture(t)
	w := newWork(t, "AI拡大")
	up := model.CatalogWorkCover{WorkID: w, ImageHash: strings.Repeat("e", 64), PortraitPinned: true, SourceID: model.SourceUpscale}
	require.NoError(t, testDB.Create(&up).Error)
	addCover(t, w, srcDLsite, "main", strings.Repeat("f", 64), model.SexualExplicit)
	assert.True(t, gradeOf(t, w), "a machine pin on an upscale is not an editor's choice")

	ids, err := CoverArtAllExplicitSQL()
	require.NoError(t, err)
	var hit int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM (`+ids+`) q WHERE work_id = ?`, w).Scan(&hit).Error)
	assert.Equal(t, int64(1), hit, "the audit query and the trigger agree")
}
