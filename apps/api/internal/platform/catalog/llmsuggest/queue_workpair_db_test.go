package llmsuggest

import (
	"fmt"
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

// The dossier's own ref list reads link_kind 0 and 1 only. official_site is
// stored as link_kind 2, which is how 60,543 rows -- and with them every
// shared-product-page signal the pair queue has -- stayed invisible to the
// model while it wrote "no shared refs" about pairs holding the same URL.
func TestSharedRefsSeeRelatedLinksAndCarryTheirFanOut(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	var siteSource int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'official_site'`).Scan(&siteSource).Error)
	require.NotZero(t, siteSource)

	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	mkRef := func(id int64, ext string) {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: id, SourceID: siteSource,
			ExternalID: ext, LinkKind: model.LinkKindRelated,
		}).Error)
	}
	mkCandidate := func(a, b int64) {
		require.NoError(t, db.Create(&model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: min(a, b), BID: max(a, b),
			Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
		}).Error)
	}

	exclusiveA, exclusiveB := mkWork("product page A"), mkWork("product page B")
	brandA, brandB := mkWork("brand title A"), mkWork("brand title B")
	brandC, brandD := mkWork("brand title C"), mkWork("brand title D")

	for _, id := range []int64{exclusiveA, exclusiveB} {
		mkRef(id, "www.appetite-game.com/apt296/apt296.html")
	}
	for _, id := range []int64{brandA, brandB, brandC, brandD} {
		mkRef(id, "frontwing.jp")
	}
	mkCandidate(exclusiveA, exclusiveB)
	mkCandidate(brandA, brandB)

	items, _, err := loadWorkPairQueue(db)
	require.NoError(t, err)
	require.Len(t, items, 2)

	got := map[int64]workPairDossier{}
	for _, it := range items {
		got[it.AID] = it.Dossier
	}
	exclusive := got[min(exclusiveA, exclusiveB)]
	require.Len(t, exclusive.SharedRefs, 1, "a related-kind link is still a shared identifier")
	require.Equal(t, "official_site:www.appetite-game.com/apt296/apt296.html", exclusive.SharedRefs[0].Ref)
	require.Equal(t, 2, exclusive.SharedRefs[0].WorksHolding, "exclusive to this pair")

	brand := got[min(brandA, brandB)]
	require.Len(t, brand.SharedRefs, 1)
	require.Equal(t, 4, brand.SharedRefs[0].WorksHolding,
		"the same field, counted: a brand root every title carries")
}

// A shared identifier the display cap dropped would read as no shared
// identifier at all, so the intersection is computed over the untruncated set.
// Adding related links is what made the cap reachable: a work carrying eight
// registry anchors plus an official site now overflows it.
//
// The shared ref here has to be link_kind related, and not because that is the
// tidier fixture: uq_catalog_external_ref_exact is a partial unique over
// link_kind 0, so two works sharing an exact ref cannot be inserted at all.
// Corroboration is only expressible above that tier, which is the same reason
// the fan-out signal exists there and nowhere else.
func TestSharedRefsSurviveTheDossierDisplayCap(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	var srcIDs []int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source ORDER BY key LIMIT 8`).Scan(&srcIDs).Error)
	require.Len(t, srcIDs, 8)
	var siteSource int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'official_site'`).Scan(&siteSource).Error)

	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	a, b := mkWork("padded A"), mkWork("padded B")
	for i, src := range srcIDs {
		for _, id := range []int64{a, b} {
			require.NoError(t, db.Create(&model.CatalogExternalRef{
				EntityType: model.EntityTypeWork, EntityID: id, SourceID: src,
				ExternalID: fmt.Sprintf("filler-%d-%d", i, id), LinkKind: model.LinkKindExact,
			}).Error)
		}
	}
	for _, id := range []int64{a, b} {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: id, SourceID: siteSource,
			ExternalID: "shared-by-both", LinkKind: model.LinkKindRelated,
		}).Error)
	}
	require.NoError(t, db.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: min(a, b), BID: max(a, b),
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
	}).Error)

	items, _, err := loadWorkPairQueue(db)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Dossier.A.Refs, 8, "the display list is still capped")
	require.Len(t, items[0].Dossier.SharedRefs, 1,
		"the shared ref sorts ninth and the cap must not hide it")
	require.Equal(t, 2, items[0].Dossier.SharedRefs[0].WorksHolding)
}

func TestDeferredCandidatesAreLoadedForJudging(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	mkCandidate := func(a, b int64, status int16) {
		require.NoError(t, db.Create(&model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: min(a, b), BID: max(a, b),
			Reason: model.CandidateReasonNameNormEqual, Status: status,
		}).Error)
	}
	dA, dB := mkWork("deferred A"), mkWork("deferred B")
	nA, nB := mkWork("needs A"), mkWork("needs B")
	mkCandidate(dA, dB, model.CandidateStatusDeferred)
	mkCandidate(nA, nB, model.CandidateStatusNeedsManual)

	items, _, err := loadWorkPairQueue(db)
	require.NoError(t, err)
	require.Len(t, items, 2)
}
