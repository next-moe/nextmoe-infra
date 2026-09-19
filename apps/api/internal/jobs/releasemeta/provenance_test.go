package releasemeta

import (
	"context"
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func provWorkRating(t *testing.T, id int64) (int16, string) {
	t.Helper()
	var w model.CatalogWork
	require.NoError(t, testDB.First(&w, id).Error)
	return w.ContentRating, provenance.FirstSource(w.FieldProvenance, "content_rating")
}

func TestFillRatingSkipsHumanRuledContentRating(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	ctx := context.Background()
	medium := galgameMedium(t)

	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterWork(reg, testDB))
	e := editing.NewEngine(testDB, reg)
	actor := editing.PolicyContext{
		UserID: 100, Site: "kungal",
		HasPerm: func(key string) bool { return perm.Resolver.Can([]string{"ren"}, authz.Permission(key)) },
	}

	ruled := mkWork(t, medium, "human ruled all-ages", nil, nil, model.ContentRatingR18)
	untouched := mkWork(t, medium, "never edited", nil, nil, model.ContentRatingAllAges)
	machine := mkWork(t, medium, "machine stamped", nil, nil, model.ContentRatingAllAges)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET field_provenance =
		 '{"content_rating":[{"source":"dlsite","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`,
		machine).Error)

	_, rev, err := e.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: ruled,
		Patch: map[string]any{editspec.FieldWorkContentRating: float64(model.ContentRatingAllAges)},
		Actor: actor,
	})
	require.NoError(t, err)
	require.NotNil(t, rev, "the kungal overlay automerges a reviewer's own proposal")

	rating, source := provWorkRating(t, ruled)
	require.Equal(t, model.ContentRatingAllAges, rating)
	require.Equal(t, provenance.SourceCurated, source, "the engine must stamp before the importer runs")

	w := &writer{db: testDB, stats: &Stats{}}
	for _, id := range []int64{ruled, untouched, machine} {
		w.fillRating(ctx, id, model.ContentRatingR18, true)
	}

	rating, source = provWorkRating(t, ruled)
	assert.Equal(t, model.ContentRatingAllAges, rating,
		"an explicit human all_ages ruling is not an unset content_rating")
	assert.Equal(t, provenance.SourceCurated, source, "the stamp must survive the importer pass")

	rating, _ = provWorkRating(t, untouched)
	assert.Equal(t, model.ContentRatingR18, rating, "an unset rating is still filled from upstream")

	rating, source = provWorkRating(t, machine)
	assert.Equal(t, model.ContentRatingR18, rating, "a machine stamp does not protect the field")
	assert.Equal(t, "dlsite", source)

	assert.Equal(t, 2, w.stats.RatingFilled)
	assert.Equal(t, 1, w.stats.RatingSkippedNonEmpty)
	assert.Zero(t, w.stats.Errors)
}

func TestFillDateSkipsEngineClearedDate(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE edit_proposal_amendment, edit_proposal, edit_revision RESTART IDENTITY CASCADE").Error)
	ctx := context.Background()
	medium := galgameMedium(t)

	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterRelease(reg, testDB))
	e := editing.NewEngine(testDB, reg)
	actor := editing.PolicyContext{
		UserID: 100, Site: "kungal",
		HasPerm: func(key string) bool { return perm.Resolver.Can([]string{"ren"}, authz.Permission(key)) },
	}

	ruledWork := mkWork(t, medium, "human cleared date", nil, nil, model.ContentRatingSensitive)
	ruled := mkRelease(t, ruledWork, 2001, 2, 3)
	controlWork := mkWork(t, medium, "never edited date", nil, nil, model.ContentRatingSensitive)
	control := mkRelease(t, controlWork, 0, 0, 0)

	regSrc, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	mkReleaseAnchor(t, ruled, "RJ030001", regSrc.dlsiteSource)
	mkDlWorkFull(t, "RJ030001", "2020-06-07 00:00:00", "2020-06-07 00:00:00+00", "")
	mkReleaseAnchor(t, control, "RJ030002", regSrc.dlsiteSource)
	mkDlWorkFull(t, "RJ030002", "2020-06-07 00:00:00", "2020-06-07 00:00:00+00", "")

	_, rev, err := e.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: editspec.TypeRelease, EntityID: ruled,
		Patch: map[string]any{editspec.FieldReleaseReleased: nil},
		Actor: actor,
	})
	require.NoError(t, err)
	require.NotNil(t, rev, "the kungal overlay automerges a reviewer's own proposal")
	require.Equal(t, provenance.SourceCurated, provenance.FirstSource(
		reloadReleaseProv(t, ruled), "released_y"))

	st, err := Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 1, st.DatesHuman)

	y, _, _ := relDate(t, ruled)
	assert.Nil(t, y, "an engine-cleared date must survive releasemeta")
	cy, cm, cd := relDate(t, control)
	require.NotNil(t, cy)
	assert.Equal(t, int16(2020), *cy)
	require.NotNil(t, cm)
	assert.Equal(t, int16(6), *cm)
	require.NotNil(t, cd)
	assert.Equal(t, int16(7), *cd)
	assert.Equal(t, 1, st.DlDateFilled)
	assert.Equal(t, 1, st.DatesWritten)
}

func reloadReleaseProv(t *testing.T, id int64) []byte {
	t.Helper()
	var rel model.CatalogRelease
	require.NoError(t, testDB.Unscoped().Select("id", "field_provenance").First(&rel, id).Error)
	return rel.FieldProvenance
}
