package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWorkPairDB(t *testing.T) (*gorm.DB, int16) {
	t.Helper()
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		`TRUNCATE catalog_merge_proposal, catalog_match_candidate, catalog_work_title, catalog_external_ref, catalog_release, catalog_work RESTART IDENTITY CASCADE`).Error)
	require.NoError(t, db.Exec("TRUNCATE src_llm.queue_verdict RESTART IDENTITY").Error)
	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	return db, medium
}

func createLiveWork(t *testing.T, db *gorm.DB, medium int16, name string) int64 {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, db.Create(w).Error)
	return w.ID
}

func fileWorkPair(t *testing.T, db *gorm.DB, a, b int64, verdict, hash string) {
	t.Helper()
	lo, hi := min(a, b), max(a, b)
	require.NoError(t, db.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: lo, BID: hi,
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
	}).Error)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork,
		AID: lo, BID: hi, InputHash: hash, Model: "test", PromptVersion: PromptWorkPair,
		Verdict: verdict, Confidence: 1, Evidence: []byte(`{}`),
	}).Error)
}

func pairStatus(t *testing.T, db *gorm.DB, a, b int64) int16 {
	t.Helper()
	var got int16
	require.NoError(t, db.Raw(`SELECT status FROM catalog_match_candidate
		WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
		model.EntityTypeWork, min(a, b), max(a, b)).Scan(&got).Error)
	return got
}

func TestStrippedKeyCorroboratesDecoratedTwin(t *testing.T) {
	db, medium := setupWorkPairDB(t)
	a := createLiveWork(t, db, medium, "【スマホ版】ヴァルキリーの剣")
	b := createLiveWork(t, db, medium, "ヴァルキリーの剣")
	fileWorkPair(t, db, a, b, VerdictSame, "hash-stripped-twin")

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusAccepted, pairStatus(t, db, a, b))
}

func TestStrippedKeyBlockedByThirdHolderTitle(t *testing.T) {
	db, medium := setupWorkPairDB(t)
	a := createLiveWork(t, db, medium, "【スマホ版】ヴァルキリーの剣")
	b := createLiveWork(t, db, medium, "ヴァルキリーの剣")
	c := createLiveWork(t, db, medium, "unrelated third host")
	require.NoError(t, db.Create(&model.CatalogWorkTitle{
		WorkID: c, Lang: "ja", Title: "ヴァルキリーの剣", Kind: model.WorkTitleKindAlias,
	}).Error)
	rows := []QueueVerdict{{ID: 1, AID: min(a, b), BID: max(a, b)}}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.False(t, ev[1].exclusiveStripped())
	assert.Equal(t, 3, ev[1].StrippedHolders)
}

func TestStrippedKeyIgnoresSecondaryTitles(t *testing.T) {
	db, medium := setupWorkPairDB(t)
	a := createLiveWork(t, db, medium, "ラブレッスン")
	b := createLiveWork(t, db, medium, "Lessons in Love")
	require.NoError(t, db.Create(&model.CatalogWorkTitle{
		WorkID: a, Lang: "en", Title: "Lessons in Love", Kind: model.WorkTitleKindAlias,
	}).Error)
	rows := []QueueVerdict{{ID: 1, AID: min(a, b), BID: max(a, b)}}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.Empty(t, ev[1].StrippedName)
	assert.False(t, ev[1].exclusiveStripped())
}

func TestStrippedKeyTooShort(t *testing.T) {
	db, medium := setupWorkPairDB(t)
	a := createLiveWork(t, db, medium, "【PC版】WXYZ")
	b := createLiveWork(t, db, medium, "WXYZ")
	rows := []QueueVerdict{{ID: 1, AID: min(a, b), BID: max(a, b)}}
	ev, err := loadPairEvidence(db, rows)
	require.NoError(t, err)
	assert.Empty(t, ev[1].StrippedName)
	assert.False(t, ev[1].exclusiveStripped())
}

func TestStrippedDoesNotOverrideContradictingExact(t *testing.T) {
	db, medium := setupWorkPairDB(t)
	var vndb int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndb).Error)
	a := createLiveWork(t, db, medium, "【スマホ版】ヴァルキリーの剣")
	b := createLiveWork(t, db, medium, "ヴァルキリーの剣")
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: a, SourceID: vndb,
		ExternalID: "v1", LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: b, SourceID: vndb,
		ExternalID: "v2", LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
	fileWorkPair(t, db, a, b, VerdictSame, "hash-stripped-conflict")

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Counts["applied_"+stampRefConflict], "counts: %v", st.Counts)
	assert.Zero(t, st.Counts["applied_"+applyAccept])
	assert.Equal(t, model.CandidateStatusRejected, pairStatus(t, db, a, b))
}

func TestKeptApartPairAcceptedOnceStrippedEvidenceAppears(t *testing.T) {
	db, medium := setupWorkPairDB(t)
	a := createLiveWork(t, db, medium, "nightstripped A")
	b := createLiveWork(t, db, medium, "nightstripped B")
	fileWorkPair(t, db, a, b, VerdictSame, "hash-night-stripped")

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Counts["applied_"+stampKeptApartUncorroborated], "counts: %v", st.Counts)

	require.NoError(t, db.Model(&model.CatalogWork{}).Where("id = ?", b).
		Update("display_name", "【スマホ版】nightstripped A").Error)

	st, err = RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusAccepted, pairStatus(t, db, a, b))
}
