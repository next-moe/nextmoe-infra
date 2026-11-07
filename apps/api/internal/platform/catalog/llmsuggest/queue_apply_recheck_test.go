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

type recheckFixture struct {
	t      *testing.T
	db     *gorm.DB
	medium int16
	eg     int16
}

func newRecheckFixture(t *testing.T) *recheckFixture {
	db := testCatalogDB(t)
	require.NoError(t, migrate.Run(db))
	require.NoError(t, seed.Run(db))
	require.NoError(t, db.Exec(
		"TRUNCATE catalog_merge_proposal, catalog_revision, catalog_match_candidate, catalog_work_title, catalog_external_ref, catalog_work RESTART IDENTITY CASCADE").Error)
	f := &recheckFixture{t: t, db: db}
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&f.medium).Error)
	require.NoError(t, db.Raw(`SELECT id FROM catalog_source WHERE key = 'erogamescape'`).Scan(&f.eg).Error)
	return f
}

func (f *recheckFixture) work(name string, status int16) int64 {
	w := &model.CatalogWork{
		MediumID: f.medium, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: status,
	}
	require.NoError(f.t, f.db.Create(w).Error)
	return w.ID
}

func (f *recheckFixture) sharedEG(exact, related int64, ext string) {
	require.NoError(f.t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: exact, SourceID: f.eg,
		ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
	require.NoError(f.t, f.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: related, SourceID: f.eg,
		ExternalID: ext, LinkKind: model.LinkKindRelated, MatchedBy: matchedByEGXlinkTwin,
	}).Error)
}

// accepted files the state an earlier night's apply and approve leave behind:
// an accepted candidate, a verdict stamped accept, an approved proposal.
func (f *recheckFixture) accepted(source, target int64, verdict string, conf float64, hash string) (verdictID, proposalID int64) {
	lo, hi := min(source, target), max(source, target)
	require.NoError(f.t, f.db.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: lo, BID: hi,
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusAccepted,
	}).Error)
	v := &QueueVerdict{
		Queue: QueueWorkPair, Lane: LaneLLM, EntityType: model.EntityTypeWork,
		AID: lo, BID: hi, InputHash: hash, Model: "test", PromptVersion: PromptWorkPair,
		Verdict: verdict, Confidence: conf, Evidence: []byte(`{}`), AppliedAction: applyAccept,
	}
	require.NoError(f.t, f.db.Create(v).Error)
	p := &model.CatalogMergeProposal{
		EntityType: model.EntityTypeWork, SourceEntityID: source, TargetEntityID: target,
		Status: model.ProposalStatusApproved, ProposedBy: 1,
		Note: applyNote(v.ID, conf) + ` sole holders of "x"` + "\napproved by user 1",
	}
	require.NoError(f.t, f.db.Create(p).Error)
	return v.ID, p.ID
}

func (f *recheckFixture) apply(dry bool) map[string]int {
	st, err := RunApply(f.t.Context(), f.db, StagingDBs{}, testQueueService(f.db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidence: 0.9, MinConfidenceReject: 0.7, DryRun: dry,
	})
	require.NoError(f.t, err)
	return st.Counts
}

func (f *recheckFixture) int16Of(sql string, args ...any) int16 {
	var v int16
	require.NoError(f.t, f.db.Raw(sql, args...).Scan(&v).Error)
	return v
}

func (f *recheckFixture) stampOf(id int64) string {
	var s string
	require.NoError(f.t, f.db.Raw(`SELECT applied_action FROM src_llm.queue_verdict WHERE id = ?`, id).Scan(&s).Error)
	return s
}

func TestRecheckWithdrawsAnAcceptTheCurrentRulesRefuse(t *testing.T) {
	f := newRecheckFixture(t)
	live := f.work("Roommates", model.WorkStatusLive)
	held := f.work("Roommates", model.WorkStatusQuarantine)
	vid, pid := f.accepted(held, live, VerdictUnsure, 0.6, "hash-stale")

	counts := f.apply(true)
	assert.Equal(t, 1, counts["would_"+recheckWithdrawn], "counts: %v", counts)
	assert.Equal(t, model.ProposalStatusApproved, f.int16Of(`SELECT status FROM catalog_merge_proposal WHERE id = ?`, pid))

	counts = f.apply(false)
	assert.Equal(t, 1, counts[recheckWithdrawn], "counts: %v", counts)
	assert.Equal(t, model.ProposalStatusRejected, f.int16Of(`SELECT status FROM catalog_merge_proposal WHERE id = ?`, pid))
	assert.Equal(t, model.CandidateStatusNeedsManual, f.int16Of(
		`SELECT status FROM catalog_match_candidate WHERE a_id = ? AND b_id = ?`, min(live, held), max(live, held)))
	assert.Equal(t, model.WorkStatusQuarantine, f.int16Of(`SELECT status FROM catalog_work WHERE id = ?`, held))
	assert.Equal(t, stampWithdrawnOnRecheck, f.stampOf(vid))

	counts = f.apply(false)
	assert.Zero(t, counts[recheckWithdrawn]+counts["applied"], "a withdrawn pair waits for a person: %v", counts)
}

func TestRecheckKeepsAnAcceptTheCurrentRulesStillMake(t *testing.T) {
	f := newRecheckFixture(t)
	a := f.work("twin A", model.WorkStatusLive)
	b := f.work("twin B", model.WorkStatusQuarantine)
	f.sharedEG(a, b, "twin-1")
	vid, pid := f.accepted(b, a, VerdictUnsure, 0, "hash-kept")

	counts := f.apply(false)
	assert.Equal(t, 1, counts[recheckKept], "counts: %v", counts)
	assert.Equal(t, model.ProposalStatusApproved, f.int16Of(`SELECT status FROM catalog_merge_proposal WHERE id = ?`, pid))
	assert.Equal(t, applyAccept, f.stampOf(vid))
}

func TestRecheckRefilesAnAcceptPointingAtTheWrongSurvivor(t *testing.T) {
	f := newRecheckFixture(t)
	a := f.work("twin A", model.WorkStatusLive)
	b := f.work("twin B", model.WorkStatusLive)
	f.sharedEG(a, b, "twin-1")
	vid, pid := f.accepted(a, b, VerdictSame, 1, "hash-redirect")

	counts := f.apply(false)
	assert.Equal(t, 1, counts[recheckRedirected], "counts: %v", counts)
	assert.Equal(t, 1, counts["applied_"+applyAccept], "counts: %v", counts)
	assert.Equal(t, model.ProposalStatusRejected, f.int16Of(`SELECT status FROM catalog_merge_proposal WHERE id = ?`, pid))

	var refiled struct{ Source, Target int64 }
	require.NoError(t, f.db.Raw(`SELECT source_entity_id AS source, target_entity_id AS target FROM catalog_merge_proposal
		WHERE status = ? AND id <> ?`, model.ProposalStatusOpen, pid).Scan(&refiled).Error)
	assert.Equal(t, b, refiled.Source)
	assert.Equal(t, a, refiled.Target)
	assert.Equal(t, model.CandidateStatusAccepted, f.int16Of(
		`SELECT status FROM catalog_match_candidate WHERE a_id = ? AND b_id = ?`, min(a, b), max(a, b)))
	assert.Equal(t, applyAccept, f.stampOf(vid))
}
