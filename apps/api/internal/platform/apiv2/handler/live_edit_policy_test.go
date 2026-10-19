package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"
	"api/internal/platform/settings/keys"

	"github.com/stretchr/testify/require"
)

func liveEditQuota(t *testing.T, env *liveEnv, token string) repr.EditQuota {
	t.Helper()
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/edit-quota", token, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var q repr.EditQuota
	require.NoError(t, json.Unmarshal(raw, &q), string(raw))
	return q
}

func liveFileProposal(t *testing.T, env *liveEnv, token string, workID int64, name string) (int, liveProposalBody, []byte) {
	t.Helper()
	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/proposals", token, fmt.Sprintf(
		`{"entity_type":%q,"entity_id":%q,"patch":{%q:%q}}`,
		editspec.TypeWork, idstr(workID), editspec.FieldWorkDisplayName, name))
	var prop liveProposalBody
	if status == http.StatusCreated {
		require.NoError(t, json.Unmarshal(raw, &prop), string(raw))
	}
	return status, prop, raw
}

func seedOpenProposals(t *testing.T, env *liveEnv, proposerUID int64, n int64) {
	t.Helper()
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM edit_proposal WHERE note = ?`, quotaProbeReason).Error)
	})
	for range n {
		require.NoError(t, env.db.Exec(`
			INSERT INTO edit_proposal
				(entity_family, entity_type, entity_id, base_revision_seq, patch,
				 proposer_uid, note, site, status, decision_note, created_at, updated_at)
			VALUES ('catalog', 'catalog.work', 1, 0, '{}'::jsonb, ?, ?, ?, 0, '', now(), now())`,
			proposerUID, quotaProbeReason, liveSite).Error)
	}
}

func seedDirectEdits(t *testing.T, env *liveEnv, actorUID int64, n int64) {
	t.Helper()
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(
			`DELETE FROM edit_revision WHERE entity_type = 'catalog.quota_probe'`).Error)
	})
	for i := range n {
		require.NoError(t, env.db.Exec(`
			INSERT INTO edit_revision
				(entity_family, entity_type, entity_id, seq, action, changed_fields, snapshot, actor_uid, site, created_at)
			VALUES ('catalog', 'catalog.quota_probe', ?, 1, ?, '[]'::jsonb, '{}'::jsonb, ?, ?, now())`,
			900000+i, editing.ActionDirect, actorUID, liveSite).Error)
	}
}

func TestLiveEditQuotaTiers(t *testing.T) {
	env := liveCatalog(t)

	user := liveEditQuota(t, env, livePlainToken)
	require.Equal(t, "user", user.QuotaTier)
	require.Equal(t, keys.CatalogProposalsPerDay.Get(), *user.Proposals.Limit)
	require.Zero(t, *user.DirectEdits.Limit, "a user applies nothing without review")
	require.Equal(t, keys.CatalogOpenProposalsMaxUser.Get(), *user.OpenProposals.Limit)
	require.Equal(t, int(keys.CatalogWriteQuotaWindowHours.Get()), user.WindowHours)

	editor := liveEditQuota(t, env, liveEditorToken)
	require.Equal(t, "editor", editor.QuotaTier)
	require.Equal(t, keys.CatalogProposalsPerDayEditor.Get(), *editor.Proposals.Limit)
	require.Equal(t, keys.CatalogDirectEditsPerDayEditor.Get(), *editor.DirectEdits.Limit)

	reviewer := liveEditQuota(t, env, liveUserToken)
	require.Equal(t, "reviewer", reviewer.QuotaTier)
	require.Equal(t, keys.CatalogProposalsPerDayReviewer.Get(), *reviewer.Proposals.Limit)
	require.Nil(t, reviewer.DirectEdits.Limit, "a reviewer's direct edits are bounded only by the proposal limit")
	require.Nil(t, reviewer.DirectEdits.Remaining)
	require.Nil(t, reviewer.OpenProposals.Limit, "the reviewer cap defaults to none")

	third := liveEditQuota(t, env, liveThirdPartyToken)
	require.Equal(t, "user", third.QuotaTier, "a developer-owned app's token is never above the user tier")
}

func TestLiveOpenProposalCapRefusesFiling(t *testing.T) {
	env := liveCatalog(t)
	limit := keys.CatalogOpenProposalsMaxUser.Get()
	require.Positive(t, limit, "this test needs a user cap")
	require.Less(t, limit, keys.CatalogProposalsPerDay.Get(),
		"the cap must bite before the daily limit or the test cannot tell them apart")
	seedOpenProposals(t, env, liveSecondPlainUID, limit)

	status, _, raw := liveFileProposal(t, env, liveSecondPlainToken, env.fx.Work, "Open Cap Probe")
	require.Equal(t, http.StatusTooManyRequests, status, string(raw))
	p := liveProblem(t, raw)
	require.Equal(t, "QUOTA_EXCEEDED", p.Code)
	require.Contains(t, p.Detail, "awaiting a decision")

	q := liveEditQuota(t, env, liveSecondPlainToken)
	require.Equal(t, limit, q.OpenProposals.Used)
	require.Zero(t, *q.OpenProposals.Remaining)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/work?entity_id="+idstr(env.fx.Work), liveSecondPlainToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var caps repr.EditCapabilities
	require.NoError(t, json.Unmarshal(raw, &caps))
	require.True(t, caps.QuotaExhausted)
	for _, f := range caps.Fields {
		require.Falsef(t, f.CanPropose, "%s: an exhausted tier proposes nothing", f.Key)
	}

	require.NoError(t, env.db.Exec(`UPDATE edit_proposal SET status = 3 WHERE note = ?`, quotaProbeReason).Error)
	status, prop, raw := liveFileProposal(t, env, liveSecondPlainToken, env.fx.Work, "Open Cap Probe")
	require.Equal(t, http.StatusCreated, status, "the same filing goes through once the backlog is withdrawn: %s", raw)
	require.Equal(t, "open", prop.State)
}

func TestLiveEditorDirectEditsUntilTheAllowanceThenQueues(t *testing.T) {
	env := liveCatalog(t)
	work := liveClaimedBy(t, env, "Editor Allowance Probe", 30201, livePlainUID)

	status, prop, raw := liveFileProposal(t, env, liveEditorToken, work, "Editor Direct")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.Equal(t, "merged", prop.State, "an editor's work edit applies without review")

	seedDirectEdits(t, env, liveEditorUID, keys.CatalogDirectEditsPerDayEditor.Get())

	q := liveEditQuota(t, env, liveEditorToken)
	require.Zero(t, *q.DirectEdits.Remaining)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/work?entity_id="+idstr(work), liveEditorToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var caps repr.EditCapabilities
	require.NoError(t, json.Unmarshal(raw, &caps))
	require.Equal(t, "editor", caps.QuotaTier)
	require.False(t, caps.QuotaExhausted, "the allowance demotes, it does not block")
	for _, f := range caps.Fields {
		if f.Key == editspec.FieldWorkDisplayName {
			require.True(t, f.CanPropose)
			require.False(t, f.WouldAutomerge, "past the allowance the edit queues")
		}
	}

	status, prop, raw = liveFileProposal(t, env, liveEditorToken, work, "Editor Queued")
	require.Equal(t, http.StatusCreated, status, string(raw))
	require.Equal(t, "open", prop.State, "past the allowance an editor's edit becomes an ordinary proposal")
	require.Equal(t, "Editor Direct", liveWorkName(t, env, work))
}

func TestLiveEditorsReviewNothing(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "Editor Review Probe", 30202)
	prop := liveOpenProposal(t, env, work, "Editor Review Renamed")

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/moderation/proposals/"+prop.ID, liveEditorToken, "")
	require.Equal(t, http.StatusForbidden, status, string(body))
	require.Equal(t, "PERMISSION_REQUIRED", liveProblemCode(t, body))
}

func liveProposalOnSite(t *testing.T, env *liveEnv, site string, workID int64, name string) *editing.Proposal {
	t.Helper()
	prop, rev, err := env.cat.Engine.CreateProposal(context.Background(), editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: workID,
		Patch: map[string]any{editspec.FieldWorkDisplayName: name},
		Actor: editing.PolicyContext{UserID: liveSecondPlainUID, Site: site},
	})
	require.NoError(t, err)
	require.Nil(t, rev)
	return prop
}

func TestLiveCentralEditorCrossesTheTenantFences(t *testing.T) {
	env := liveCatalog(t)
	work := liveClaimedBy(t, env, "Central Fence Probe", 30203, livePlainUID)
	prop := liveProposalOnSite(t, env, liveOtherSite, work, "Central Fence Renamed")
	path := "/v2/moderation/proposals/" + idstr(prop.ID)

	status, _, body := liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, http.StatusForbidden, status, "a downstream site keeps its fence: %s", body)
	require.Equal(t, "TENANT_MISMATCH", liveProblemCode(t, body))

	status, _, body = liveDo(t, env, http.MethodGet, path, liveCentralToken, "")
	require.Equal(t, http.StatusOK, status, "the central editor reads another site's proposal: %s", body)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/proposals?object=work&entity_id="+idstr(work), liveCentralToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var page struct {
		Items []liveProposalBody `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1, "the central queue spans sites: %s", body)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/proposals?object=work&entity_id="+idstr(work), liveUserToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	page.Items = nil
	require.NoError(t, json.Unmarshal(body, &page))
	require.Empty(t, page.Items, "a downstream queue lists only its own site's proposals")

	etag := liveETag(t, env, path, liveCentralToken)
	status, _, body = liveDoHeader(t, env, http.MethodPost, path+"/decisions", liveCentralToken,
		`{"decision":"merge"}`, map[string]string{"If-Match": etag})
	require.Equal(t, http.StatusCreated, status, "the central editor decides it: %s", body)
	require.Equal(t, "Central Fence Renamed", liveWorkName(t, env, work))

	status, later, body := liveFileProposal(t, env, liveCentralToken, work, "Central Later Name")
	require.Equal(t, http.StatusCreated, status, string(body))
	require.Equal(t, "merged", later.State, "a moderator's work edit applies at once")

	require.NoError(t, env.db.Exec(`UPDATE catalog_work SET site = ? WHERE id = ?`, liveOtherSite, work).Error)
	var revID int64
	require.NoError(t, env.db.Raw(`SELECT id FROM edit_revision WHERE entity_type = ? AND entity_id = ? ORDER BY seq LIMIT 1`,
		editspec.TypeWork, work).Scan(&revID).Error)
	revert := fmt.Sprintf(`{"revision_id":%q,"reason":"central"}`, idstr(revID))
	status, _, body = liveDo(t, env, http.MethodPost, "/v2/moderation/reverts", liveUserToken, revert)
	require.Equal(t, http.StatusForbidden, status, "a downstream site may not revert another site's claim: %s", body)
	require.Equal(t, "TENANT_MISMATCH", liveProblemCode(t, body))
	status, _, body = liveDo(t, env, http.MethodPost, "/v2/moderation/reverts", liveCentralToken, revert)
	require.Equal(t, http.StatusCreated, status, "the central editor reverts it: %s", body)
	require.Equal(t, "Central Fence Renamed", liveWorkName(t, env, work))
}

func TestLiveCentralEditorListsMyProposalsOnEverySite(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "Central Mine Probe", 30204)
	prop, rev, err := env.cat.Engine.CreateProposal(context.Background(), editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work,
		Patch: map[string]any{editspec.FieldWorkOLang: "en"},
		Actor: editing.PolicyContext{UserID: liveCentralUID, Site: liveSite},
	})
	require.NoError(t, err)
	require.Nil(t, rev)

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/me/proposals?object=work&entity_id="+idstr(work), liveCentralToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var page struct {
		Items []liveProposalBody `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1, "a proposal filed on kungal shows on the central editor: %s", body)
	require.Equal(t, idstr(prop.ID), page.Items[0].ID)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/work?entity_id="+idstr(work), liveCentralToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var caps repr.EditCapabilities
	require.NoError(t, json.Unmarshal(body, &caps))
	require.True(t, caps.Central)
	require.Equal(t, "reviewer", caps.QuotaTier, "a moderator reviews")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/edit-capabilities/work", liveUserToken, "")
	require.Equal(t, http.StatusOK, status, string(body))
	caps = repr.EditCapabilities{}
	require.NoError(t, json.Unmarshal(body, &caps))
	require.False(t, caps.Central)
}
