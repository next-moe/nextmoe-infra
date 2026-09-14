package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"

	"github.com/stretchr/testify/require"
)

func TestWorkPairQueueSkipsARetiredEndpoint(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	require.NotZero(t, medium)

	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	mkCandidate := func(a, b int64) {
		require.NoError(t, db.Create(&model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: min(a, b), BID: max(a, b),
			Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
		}).Error)
	}

	liveA, liveB := mkWork("still here A"), mkWork("still here B")
	retired, survivor := mkWork("retired"), mkWork("survivor")
	require.NoError(t, db.Delete(&model.CatalogWork{}, retired).Error)

	mkCandidate(liveA, liveB)
	mkCandidate(retired, survivor)

	items, _, err := loadWorkPairQueue(db)
	require.NoError(t, err)
	require.Len(t, items, 1, "the pair naming a retired work is not judgeable")
	require.Equal(t, min(liveA, liveB), items[0].AID)
	require.Equal(t, max(liveA, liveB), items[0].BID)
}

// A quarantined work is the duplicate holding pen, so it stays in the queue:
// executing the merge is what releases it back to live.
func TestWorkPairQueueKeepsAQuarantinedEndpoint(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)

	mk := func(name string, status int16) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: status,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	held := mk("held", model.WorkStatusQuarantine)
	live := mk("live", model.WorkStatusLive)
	require.NoError(t, db.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: min(held, live), BID: max(held, live),
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
	}).Error)

	items, _, err := loadWorkPairQueue(db)
	require.NoError(t, err)
	require.Len(t, items, 1)
}
