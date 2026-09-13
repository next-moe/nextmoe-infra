package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/settings/keys"

	"github.com/stretchr/testify/require"
)

// Rows this file plants carry a reason nothing else writes, so cleanup can take
// exactly them back out. Leaving them behind would push the plain user over the
// default limit for the rest of the package.
const quotaProbeReason = "write-quota-probe"

func seedClaimWrites(t *testing.T, env *liveEnv, actorUID int64, n int64, at time.Time) {
	t.Helper()
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(
			`DELETE FROM catalog_claim_event WHERE reason = ?`, quotaProbeReason).Error)
	})
	reason := quotaProbeReason
	rows := make([]model.CatalogClaimEvent, 0, n)
	for range n {
		rows = append(rows, model.CatalogClaimEvent{
			WorkID: 1, ToState: model.ClaimStateDraft, ActorUID: actorUID,
			Site: liveSite, Reason: &reason, CreatedAt: at,
		})
	}
	require.NoError(t, env.db.Create(&rows).Error)
}

func seedProposals(t *testing.T, env *liveEnv, proposerUID int64, n int64, at time.Time) {
	t.Helper()
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(
			`DELETE FROM edit_proposal WHERE note = ?`, quotaProbeReason).Error)
	})
	for range n {
		require.NoError(t, env.db.Exec(`
			INSERT INTO edit_proposal
				(entity_family, entity_type, entity_id, base_revision_seq, patch,
				 proposer_uid, note, site, status, decision_note, created_at, updated_at)
			VALUES ('catalog', 'catalog.work', 1, 0, '{}'::jsonb, ?, ?, ?, 0, '', ?, ?)`,
			proposerUID, quotaProbeReason, liveSite, at, at).Error)
	}
}

func mintBody(name string) string {
	return fmt.Sprintf(`{"display_name":%q,"field_values":{"catalog.work.olang":"en"},"confirm_duplicates":true}`, name)
}

// The cap is on the action, for everyone. Minting needs no permission — only a
// user token bound to a catalog site — and a mint that lands `pending` is
// already in the default public works list and search.
func TestLiveClaimQuotaRefusesThePlainContributorAtTheLimit(t *testing.T) {
	env := liveCatalog(t)
	limit := keys.CatalogClaimWritesPerDay.Get()

	seedClaimWrites(t, env, livePlainUID, limit, time.Now().Add(-time.Hour))

	status, _, p := liveCreateClaim(t, env, livePlainToken, mintBody("Quota Probe Over"))
	require.Equal(t, http.StatusTooManyRequests, status, p.Detail)
	require.Equal(t, "QUOTA_EXCEEDED", p.Code, p.Detail)

	// The window is the separating half: without it "refused" is equally
	// explained by "this account is blocked outright". Age the same rows past
	// the window and the same request must go through.
	require.NoError(t, env.db.Exec(
		`UPDATE catalog_claim_event SET created_at = ? WHERE reason = ?`,
		time.Now().Add(-time.Duration(keys.CatalogWriteQuotaWindowHours.Get())*time.Hour-time.Hour),
		quotaProbeReason).Error)

	status, rec, p := liveCreateClaim(t, env, livePlainToken, mintBody("Quota Probe Aged Out"))
	require.Equal(t, http.StatusCreated, status, p.Detail)
	require.NotEmpty(t, rec.ID)
}

// The second half of the tier split: the same count that refuses a plain
// contributor is nowhere near the trusted limit, so it must not refuse a holder
// of catalog.claim.trusted.
func TestLiveClaimQuotaGivesTrustedHoldersTheHigherLimit(t *testing.T) {
	env := liveCatalog(t)
	plain := keys.CatalogClaimWritesPerDay.Get()
	require.Less(t, plain, keys.CatalogClaimWritesPerDayTrusted.Get(),
		"this test only means something while the trusted tier is the higher one")

	seedClaimWrites(t, env, liveUID, plain, time.Now().Add(-time.Hour))

	status, rec, p := liveCreateClaim(t, env, liveUserToken, mintBody("Quota Probe Trusted"))
	require.Equal(t, http.StatusCreated, status, p.Detail)
	require.NotEmpty(t, rec.ID)
}

func TestLiveProposalQuotaRefusesThePlainContributorAtTheLimit(t *testing.T) {
	env := liveCatalog(t)
	limit := keys.CatalogProposalsPerDay.Get()

	seedProposals(t, env, livePlainUID, limit, time.Now().Add(-time.Hour))

	body := fmt.Sprintf(`{"entity_type":"catalog.work","entity_id":"%d","patch":{"catalog.work.olang":"en"}}`, env.fx.Work)
	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/proposals", livePlainToken, body)
	require.Equal(t, http.StatusTooManyRequests, status, string(raw))
	require.Equal(t, "QUOTA_EXCEEDED", liveProblem(t, raw).Code, string(raw))
}
