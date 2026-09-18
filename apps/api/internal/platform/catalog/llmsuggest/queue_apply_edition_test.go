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

type editionRef struct {
	src int16
	ext string
}

type editionPair struct {
	db     *gorm.DB
	lo, hi int64
	hash   string
}

func setupEditionApply(t *testing.T) (*gorm.DB, int16) {
	t.Helper()
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_merge_proposal, catalog_match_candidate, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)
	require.NoError(t, db.Exec("TRUNCATE src_llm.queue_verdict RESTART IDENTITY").Error)
	var medium int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	return db, medium
}

func fileEditionPair(t *testing.T, db *gorm.DB, medium int16, name, hash, verdict string, conf float64, status int16, stamp string, aRef, bRef editionRef) editionPair {
	t.Helper()
	mkWork := func() int64 {
		w := &model.CatalogWork{
			MediumID: medium, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		}
		require.NoError(t, db.Create(w).Error)
		return w.ID
	}
	a, b := mkWork(), mkWork()
	for _, r := range []struct {
		id  int64
		ref editionRef
	}{{a, aRef}, {b, bRef}} {
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: r.id, SourceID: r.ref.src,
			ExternalID: r.ref.ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
		}).Error)
	}
	lo, hi := min(a, b), max(a, b)
	require.NoError(t, db.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: lo, BID: hi,
		Reason: model.CandidateReasonNameNormEqual, Status: status,
	}).Error)
	require.NoError(t, db.Create(&QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork,
		AID: lo, BID: hi, InputHash: hash, Model: "test", PromptVersion: PromptWorkPair,
		Verdict: verdict, Confidence: conf, Evidence: []byte(`{}`),
		AppliedAction: stamp,
	}).Error)
	return editionPair{db: db, lo: lo, hi: hi, hash: hash}
}

func (p editionPair) status(t *testing.T) int16 {
	t.Helper()
	var got int16
	require.NoError(t, p.db.Raw(`SELECT status FROM catalog_match_candidate
		WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
		model.EntityTypeWork, p.lo, p.hi).Scan(&got).Error)
	return got
}

func (p editionPair) action(t *testing.T) string {
	t.Helper()
	var got string
	require.NoError(t, p.db.Raw(`SELECT applied_action FROM src_llm.queue_verdict WHERE input_hash = ?`,
		p.hash).Scan(&got).Error)
	return got
}

func (p editionPair) proposals(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, p.db.Raw(`SELECT count(*) FROM catalog_merge_proposal`).Scan(&n).Error)
	return n
}

func applyWorkPair(t *testing.T, db *gorm.DB) ApplyStats {
	t.Helper()
	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidence: 0.9, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	return st
}

func TestEGIdConflictDoesNotVeto(t *testing.T) {
	db, medium := setupEditionApply(t)
	p := fileEditionPair(t, db, medium, "恋姫†無双", "hash-eg-veto", VerdictSame, 1,
		model.CandidateStatusNeedsManual, "",
		editionRef{model.SourceErogameScape, "1824"},
		editionRef{model.SourceErogameScape, "211785"},
	)
	st := applyWorkPair(t, db)
	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusAccepted, p.status(t))
	assert.Equal(t, int64(1), p.proposals(t))
}

func TestBangumiIdConflictDoesNotVeto(t *testing.T) {
	db, medium := setupEditionApply(t)
	p := fileEditionPair(t, db, medium, "円環の柩", "hash-bgm-veto", VerdictSame, 1,
		model.CandidateStatusNeedsManual, "",
		editionRef{model.SourceBangumi, "100"},
		editionRef{model.SourceBangumi, "200"},
	)
	st := applyWorkPair(t, db)
	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusAccepted, p.status(t))
	assert.Equal(t, int64(1), p.proposals(t))
}

func TestVNDBIdConflictStillRejects(t *testing.T) {
	db, medium := setupEditionApply(t)
	p := fileEditionPair(t, db, medium, "Faith", "hash-vndb-veto", VerdictSame, 1,
		model.CandidateStatusNeedsManual, "",
		editionRef{2, "v1"},
		editionRef{2, "v2"},
	)
	st := applyWorkPair(t, db)
	assert.Equal(t, 1, st.Counts["applied_"+stampRefConflict], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusRejected, p.status(t))
	assert.Equal(t, stampRefConflict, p.action(t))
	assert.Zero(t, p.proposals(t))
}

func TestOldRefConflictRejectIsReopenedAndAccepted(t *testing.T) {
	db, medium := setupEditionApply(t)
	p := fileEditionPair(t, db, medium, "加奈～いもうと～", "hash-reopen-eg", VerdictSame, 1,
		model.CandidateStatusRejected, stampRefConflictPrev,
		editionRef{model.SourceErogameScape, "1824"},
		editionRef{model.SourceErogameScape, "211785"},
	)
	st := applyWorkPair(t, db)
	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusAccepted, p.status(t))
	assert.Equal(t, applyAccept, p.action(t))
	assert.Equal(t, int64(1), p.proposals(t))
}

func TestOldRefConflictOnVNDBStaysRejected(t *testing.T) {
	db, medium := setupEditionApply(t)
	p := fileEditionPair(t, db, medium, "Ever17", "hash-reopen-vndb", VerdictSame, 1,
		model.CandidateStatusRejected, stampRefConflictPrev,
		editionRef{2, "v10"},
		editionRef{2, "v11"},
	)
	st := applyWorkPair(t, db)
	assert.Equal(t, 1, st.Counts["applied_"+stampRefConflict], "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusRejected, p.status(t))
	assert.Equal(t, stampRefConflict, p.action(t))
	assert.Zero(t, p.proposals(t))
}

func TestDifferentVerdictRejectIsNeverReopened(t *testing.T) {
	db, medium := setupEditionApply(t)
	p := fileEditionPair(t, db, medium, "Eclipse", "hash-plain-reject", VerdictDifferent, 1,
		model.CandidateStatusRejected, applyReject,
		editionRef{model.SourceErogameScape, "1"},
		editionRef{model.SourceErogameScape, "2"},
	)
	st := applyWorkPair(t, db)
	assert.Zero(t, st.Applied, "counts: %v", st.Counts)
	assert.Equal(t, model.CandidateStatusRejected, p.status(t))
	assert.Equal(t, applyReject, p.action(t))
	assert.Zero(t, p.proposals(t))
}
