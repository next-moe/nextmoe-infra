package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func testQueueService(db *gorm.DB) *service.AdminQueueService {
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve,
		repository.NewProposalRepository(db), repository.NewRevisionRepository(db))
	return service.NewAdminQueueService(db, merge)
}

// The screen is only worth anything if the apply loop hands it the rows. Before
// applySelection widened, the SQL filtered on the accept bar and on three
// verdicts, so an unsure pair at confidence 0 - the exact shape the screen was
// written to decide - was never loaded at all.
func TestApplyRejectsAnUnsurePairOnARefContradiction(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	var vndb, hltb int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndb).Error)
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'howlongtobeat'`).Scan(&hltb).Error)

	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	mkRef := func(id int64, src int16, ext string) {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: id, SourceID: src,
			ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
		}).Error)
	}
	mkPair := func(a, b int64, verdict string, conf float64, hash string) {
		lo, hi := min(a, b), max(a, b)
		require.NoError(t, db.Create(&model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: lo, BID: hi,
			Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
		}).Error)
		require.NoError(t, db.Create(&QueueVerdict{
			Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork,
			AID: lo, BID: hi, InputHash: hash, Model: "test", PromptVersion: PromptWorkPairV1,
			Verdict: verdict, Confidence: conf, Evidence: []byte(`{}`),
		}).Error)
	}
	status := func(a, b int64) int16 {
		var got int16
		require.NoError(t, db.Raw(`SELECT status FROM catalog_match_candidate
		        WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
			model.EntityTypeWork, min(a, b), max(a, b)).Scan(&got).Error)
		return got
	}

	contraA, contraB := mkWork("shared title A"), mkWork("shared title B")
	mkRef(contraA, vndb, "v1")
	mkRef(contraB, vndb, "v2")
	mkPair(contraA, contraB, VerdictUnsure, 0, "hash-contradicted")

	// positive control: same shape, contradiction only on a source that does not
	// deduplicate itself, so the unsure verdict still governs and nothing happens
	hltbA, hltbB := mkWork("hltb A"), mkWork("hltb B")
	mkRef(hltbA, hltb, "100")
	mkRef(hltbB, hltb, "200")
	mkPair(hltbA, hltbB, VerdictUnsure, 0, "hash-hltb")

	st, err := RunApply(t.Context(), db, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidence: 0.9, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, st.Counts["applied_"+stampRefConflict], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusRejected, status(contraA, contraB))
	assert.Equal(t, model.CandidateStatusNeedsManual, status(hltbA, hltbB))
	assert.Equal(t, 1, st.Counts[skipUnsure])

	// catalog_match_candidate keeps no note, so applied_action is the whole
	// audit trail for a pair the machine decided rather than the model
	var action string
	require.NoError(t, db.Raw(`SELECT applied_action FROM src_llm.queue_verdict
	        WHERE input_hash = 'hash-contradicted'`).Scan(&action).Error)
	assert.Equal(t, stampRefConflict, action)

	var hltbAction string
	require.NoError(t, db.Raw(`SELECT applied_action FROM src_llm.queue_verdict
	        WHERE input_hash = 'hash-hltb'`).Scan(&hltbAction).Error)
	assert.Empty(t, hltbAction)
}

// A row stamped by something other than the current rules is not done, it is
// parked by a rule that no longer exists. Selecting on an empty applied_action
// made 325 such rows invisible to every screen written afterwards, 319 of which
// held a real exact-ref contradiction.
func TestApplyReJudgesARowStampedByAVanishedRule(t *testing.T) {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_match_candidate, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)
	require.NoError(t, db.Exec("TRUNCATE src_llm.queue_verdict RESTART IDENTITY").Error)

	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	var vndb int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndb).Error)

	mkWork := func(name string) int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	a, b := mkWork("parked A"), mkWork("parked B")
	for _, r := range []struct {
		id  int64
		ext string
	}{{a, "v10"}, {b, "v11"}} {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: r.id, SourceID: vndb,
			ExternalID: r.ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
		}).Error)
	}
	lo, hi := min(a, b), max(a, b)
	require.NoError(t, db.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: lo, BID: hi,
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusNeedsManual,
	}).Error)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork,
		AID: lo, BID: hi, InputHash: "hash-parked", Model: "test", PromptVersion: PromptWorkPairV1,
		Verdict: VerdictSame, Confidence: 1, Evidence: []byte(`{}`),
		AppliedAction: "excluded_conflicting_refs",
	}).Error)

	st, err := RunApply(t.Context(), db, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidence: 0.9, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Counts["applied_"+stampRefConflict], "counts: %v", st.Counts)

	var got struct {
		Status int16
		Action string
	}
	require.NoError(t, db.Raw(`SELECT c.status, v.applied_action AS action
	        FROM catalog_match_candidate c, src_llm.queue_verdict v
	        WHERE c.entity_type = ? AND c.a_id = ? AND c.b_id = ?
	          AND v.input_hash = 'hash-parked'`,
		model.EntityTypeWork, lo, hi).Scan(&got).Error)
	assert.Equal(t, model.CandidateStatusRejected, got.Status)
	assert.Equal(t, stampRefConflict, got.Action, "the stamp write must not still guard on emptiness")

	// and a row carrying a stamp this package does write stays done
	st, err = RunApply(t.Context(), db, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidence: 0.9, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Zero(t, st.Applied, "counts: %v", st.Counts)
}
