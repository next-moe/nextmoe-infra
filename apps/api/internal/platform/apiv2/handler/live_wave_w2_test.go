package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func liveClaimedBy(t *testing.T, env *liveEnv, name string, product, actor int64) int64 {
	t.Helper()
	empty := datatypes.JSON([]byte("{}"))
	w := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		Extra: empty, FieldProvenance: empty,
	}
	require.NoError(t, env.db.Create(w).Error)
	_, err := env.cat.Claims.Act(context.Background(), catsvc.ClaimActionParams{
		WorkID: w.ID, Action: catsvc.ClaimActionClaim, Site: liveSite,
		ProductWorkID: &product, ActorUID: actor,
	})
	require.NoError(t, err)
	return w.ID
}

func liveProblemCode(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	return p.Code
}

// An open proposal needs an untrusted proposer: the admin token holds
// catalog.edit.trusted, so its own work edits auto-merge at creation and there
// is nothing left for a moderation face to decide.
func liveOpenProposal(t *testing.T, env *liveEnv, workID int64, name string) liveProposalBody {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodPost, "/v2/me/proposals", livePlainToken, fmt.Sprintf(
		`{"entity_type":%q,"entity_id":%q,"patch":{%q:%q},"note":"w2"}`,
		editspec.TypeWork, idstr(workID), editspec.FieldWorkDisplayName, name))
	require.Equal(t, 201, status, string(body))
	var prop liveProposalBody
	require.NoError(t, json.Unmarshal(body, &prop))
	require.Equal(t, "open", prop.State)
	return prop
}

func TestLiveModerationProposalsNeedReviewAuthority(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 Queue Target", 30101)
	prop := liveOpenProposal(t, env, work, "W2 Queue Renamed")

	for _, path := range []string{"/v2/moderation/proposals", "/v2/moderation/proposals/" + prop.ID} {
		status, _, body := liveDo(t, env, http.MethodGet, path, livePlainToken, "")
		require.Equal(t, 403, status, "%s: a site binding is not standing to read the queue: %s", path, body)
		require.Equal(t, "PERMISSION_REQUIRED", liveProblemCode(t, body))

		status, _, body = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
		require.Equal(t, 200, status, "positive control %s: %s", path, body)
	}

	// The patch body is the payload the gate exists for.
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/moderation/proposals/"+prop.ID+"?include=patch", livePlainToken, "")
	require.Equal(t, 403, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/moderation/proposals/"+prop.ID+"?include=patch", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var detail liveProposalBody
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotNil(t, detail.Patch, "positive control: the reviewer still gets the patch")
}

func liveProposalOn(t *testing.T, env *liveEnv, work int64, token, name string) liveProposalBody {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodPost, "/v2/me/proposals", token, fmt.Sprintf(
		`{"entity_type":%q,"entity_id":%q,"patch":{%q:%q},"note":"w2"}`,
		editspec.TypeWork, idstr(work), editspec.FieldWorkDisplayName, name))
	require.Equal(t, 201, status, string(body))
	var prop liveProposalBody
	require.NoError(t, json.Unmarshal(body, &prop))
	return prop
}

// kungal overlays every work field with OwnerReview, so a game's owner reaches
// a verdict on it holding no review permission — the forum ships that path
// (/galgame-edit/proposals/:id is plain authed, gated on CanModerate OR
// isGameOwner). The owner arm must stay pinned to the entity: granting the
// whole type's queue through it would be worse than the gap it closes.
func TestLiveProposalDetailAdmitsTheEntityOwnerAndNobodyElse(t *testing.T) {
	env := liveCatalog(t)
	mine := liveClaimedBy(t, env, "W2 Owner Mine", 30111, livePlainUID)
	theirs := liveClaimedBy(t, env, "W2 Owner Theirs", 30112, liveSecondPlainUID)
	onMine := liveProposalOn(t, env, mine, liveSecondPlainToken, "W2 Owner Mine Renamed")
	onTheirs := liveProposalOn(t, env, theirs, livePlainToken, "W2 Owner Theirs Renamed")

	detail := func(id, token string) (int, []byte) {
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/moderation/proposals/"+id+"?include=patch", token, "")
		return status, body
	}

	status, body := detail(onMine.ID, livePlainToken)
	require.Equal(t, 200, status, "the owner reads the queue entry on their own entity: %s", body)
	var got liveProposalBody
	require.NoError(t, json.Unmarshal(body, &got))
	require.NotNil(t, got.Patch)

	status, body = detail(onTheirs.ID, livePlainToken)
	require.Equal(t, 403, status, "owning one entity is not standing over another: %s", body)
	require.Equal(t, "PERMISSION_REQUIRED", liveProblemCode(t, body))

	status, body = detail(onMine.ID, liveUserToken)
	require.Equal(t, 200, status, "positive control: a reviewer reads any of them: %s", body)
}

func TestLiveModerationQueueOwnerArmIsEntityScoped(t *testing.T) {
	env := liveCatalog(t)
	mine := liveClaimedBy(t, env, "W2 Queue Owner Mine", 30113, livePlainUID)
	theirs := liveClaimedBy(t, env, "W2 Queue Owner Theirs", 30114, liveSecondPlainUID)
	onMine := liveProposalOn(t, env, mine, liveSecondPlainToken, "W2 Queue Owner Mine Renamed")
	liveProposalOn(t, env, theirs, livePlainToken, "W2 Queue Owner Theirs Renamed")

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/moderation/proposals", livePlainToken, "")
	require.Equal(t, 403, status, "the whole type's queue is never reachable through the owner arm: %s", body)
	require.Equal(t, "PERMISSION_REQUIRED", liveProblemCode(t, body))

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/moderation/proposals?object=work&entity_id="+idstr(theirs), livePlainToken, "")
	require.Equal(t, 403, status, "naming someone else's entity is not standing either: %s", body)

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/moderation/proposals?object=work&entity_id="+idstr(mine), livePlainToken, "")
	require.Equal(t, 200, status, "the owner reads their own entity's queue: %s", body)
	var page liveProposalPage
	require.NoError(t, json.Unmarshal(body, &page))
	require.NotEmpty(t, page.Items)
	for _, it := range page.Items {
		require.Equal(t, idstr(mine), it.EntityID, "the entity filter must reach the query, not only the check")
		require.Equal(t, onMine.ID, it.ID)
	}

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/moderation/proposals?entity_id="+idstr(mine), livePlainToken, "")
	require.Equal(t, 422, status, "entity ids are unique only within a family: %s", body)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/proposals", liveUserToken, "")
	require.Equal(t, 200, status, "positive control: a reviewer still reads the whole queue: %s", body)
}

// Owner-can-decide is the live behaviour: forum's Merge/Decline sit behind the
// same OR and its Amend route carries no role gate at all.
func TestLiveEntityOwnerCanStillAmendAndDecide(t *testing.T) {
	env := liveCatalog(t)
	work := liveClaimedBy(t, env, "W2 Owner Writes", 30115, livePlainUID)
	prop := liveProposalOn(t, env, work, liveSecondPlainToken, "W2 Owner Writes Renamed")
	path := "/v2/moderation/proposals/" + prop.ID

	etag := liveETag(t, env, path, livePlainToken)
	status, _, body := liveDoHeader(t, env, http.MethodPost, "/v2/me/proposals/"+prop.ID+"/amendments",
		livePlainToken, `{"set":{"catalog.work.display_name":"W2 Owner Amended"},"note":"owner"}`,
		map[string]string{"If-Match": etag})
	require.Equal(t, 201, status, "the owner amends a proposal on their own entity: %s", body)

	etag = liveETag(t, env, path, livePlainToken)
	status, _, body = liveDoHeader(t, env, http.MethodPost, path+"/decisions", livePlainToken,
		`{"decision":"merge"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 201, status, "and reaches the verdict: %s", body)
	require.Equal(t, "W2 Owner Amended", liveWorkName(t, env, work))
}

// The owner channel is part of the work and release policy, the same on every
// site since the per-site overlays went; character carries none.
func TestReviewStandingFollowsThePolicy(t *testing.T) {
	env := liveCatalog(t)
	work := liveClaimedBy(t, env, "W2 Overlay Probe", 30116, livePlainUID)
	owner := func(site string) editing.PolicyContext {
		return editing.PolicyContext{UserID: livePlainUID, Site: site, HasPerm: func(string) bool { return false }}
	}
	ctx := t.Context()

	for _, site := range []string{liveSite, "letmoe", "editor"} {
		require.True(t, env.cat.reviewsAnyField(ctx, editspec.TypeWork, work, owner(site)),
			"catalog.work carries OwnerReview on %s", site)
	}
	require.False(t, env.cat.reviewsAnyField(ctx, editspec.TypeCharacter, work, owner(liveSite)),
		"OwnerReview is on work and release only, never on character")
	require.False(t, env.cat.reviewsAnyField(ctx, editspec.TypeWork, 0, owner(liveSite)),
		"nobody owns entity 0; that is what keeps the type-level queue type-level")

	capped := owner(liveSite)
	capped.ModerationCapped = true
	require.False(t, env.cat.reviewsAnyField(ctx, editspec.TypeWork, work, capped),
		"a developer-owned app's token reaches no verdict, ownership or not")
}

// GET /v2/moderation/snapshots/{object}/{id} is not a moderation face despite
// the path: it is the editor bootstrap read, and forum, patch and letmoe all
// call it with an ordinary member's token (forum's /galgame/:gid/edit/bootstrap
// is plain authed). Gating it is an outage, so this pins the reachability the
// products depend on while the leak it carries is reported instead.
func TestLiveModerationSnapshotStaysTheEditorBootstrap(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 Snapshot Target", 30102)
	path := "/v2/moderation/snapshots/work/" + idstr(work)

	status, _, body := liveDo(t, env, http.MethodGet, path, livePlainToken, "")
	require.Equal(t, 200, status, "an ordinary contributor bootstraps the editor here: %s", body)

	status, _, body = liveDo(t, env, http.MethodGet, path, liveOtherSiteToken, "")
	require.Equal(t, 200, status, "a peer tenant's editor previews across sites by design: %s", body)

	status, _, body = liveDo(t, env, http.MethodGet, path, "", "")
	require.Equal(t, 401, status, "positive control: it still needs a user token: %s", body)
}

func TestLiveProposalWritesAreTenantFenced(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 Fence Target", 30103)
	prop := liveOpenProposal(t, env, work, "W2 Fence Renamed")
	path := "/v2/moderation/proposals/" + prop.ID
	etag := liveETag(t, env, path, liveUserToken)

	cases := []struct {
		method, path, body string
	}{
		{http.MethodPatch, "/v2/me/proposals/" + prop.ID, `{"patch":{"catalog.work.display_name":"Cross Tenant"}}`},
		{http.MethodPost, "/v2/me/proposals/" + prop.ID + "/amendments", `{"set":{"catalog.work.display_name":"Cross Tenant"}}`},
		{http.MethodPost, path + "/decisions", `{"decision":"decline","note":"not yours"}`},
	}
	for _, c := range cases {
		status, _, body := liveDoHeader(t, env, c.method, c.path, liveOtherSiteToken, c.body,
			map[string]string{"If-Match": etag})
		require.Equal(t, 403, status, "%s %s: %s", c.method, c.path, body)
		require.Equal(t, "TENANT_MISMATCH", liveProblemCode(t, body))

		// A wildcard validator matches any non-empty ETag, so it must not be
		// the way past a check the read would have refused.
		status, _, body = liveDoHeader(t, env, c.method, c.path, liveOtherSiteToken, c.body,
			map[string]string{"If-Match": "*"})
		require.Equal(t, 403, status, "%s %s with If-Match: *: %s", c.method, c.path, body)
		require.Equal(t, "TENANT_MISMATCH", liveProblemCode(t, body))
	}

	status, _, body := liveDoHeader(t, env, http.MethodPost, path+"/decisions", liveUserToken,
		`{"decision":"decline","note":"positive control"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 201, status, "positive control: the owning tenant's reviewer decides: %s", body)
}

func TestLiveProposalPatchNeedsProposerOrReviewer(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 Owner Target", 30104)
	prop := liveOpenProposal(t, env, work, "W2 Owner Renamed")
	path := "/v2/me/proposals/" + prop.ID
	etag := liveETag(t, env, "/v2/moderation/proposals/"+prop.ID, liveUserToken)

	// Same tenant, no review permission, not the proposer.
	status, _, body := liveDoHeader(t, env, http.MethodPatch, path, liveSecondPlainToken,
		`{"state":"withdrawn"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 403, status, string(body))
	require.Equal(t, "PERMISSION_REQUIRED", liveProblemCode(t, body))

	status, _, body = liveDoHeader(t, env, http.MethodPatch, path, livePlainToken,
		`{"state":"withdrawn"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 200, status, "positive control: the proposer withdraws their own: %s", body)
}

func TestLiveRevertIsTenantFencedAndReviewGated(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 Revert Target", 30105)
	liveMergedProposal(t, env, work, "W2 Revert First")
	liveMergedProposal(t, env, work, "W2 Revert Second")

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/revisions?object=work&entity_id="+idstr(work)+"&sort=recorded_asc&limit=1", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var page liveRevisionPage
	require.NoError(t, json.Unmarshal(body, &page))
	require.NotEmpty(t, page.Items)
	revID := page.Items[0].ID

	status, _, body = liveDo(t, env, http.MethodPost, "/v2/moderation/reverts", liveOtherSiteToken,
		`{"revision_id":"`+revID+`","reason":"cross tenant"}`)
	require.Equal(t, 403, status, string(body))
	require.Equal(t, "TENANT_MISMATCH", liveProblemCode(t, body),
		"RevisionByID is a bare lookup; the fence has to be the caller's")

	status, _, body = liveDo(t, env, http.MethodPost, "/v2/moderation/reverts", liveUserToken,
		`{"revision_id":"`+revID+`","reason":"positive control"}`)
	require.Equal(t, 201, status, "positive control: the owning tenant reverts: %s", body)
	require.Equal(t, "W2 Revert First", liveWorkName(t, env, work))
}

// The v1 surface set PolicyContext.ModerationCapped from the token's client and
// wave R3 deleted it, so from then on a developer-owned app's token reached
// verdicts with whatever roles its user happened to carry.
func TestLiveThirdPartyClientReachesNoVerdict(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 Capped Target", 30106)
	prop := liveOpenProposal(t, env, work, "W2 Capped Renamed")
	path := "/v2/moderation/proposals/" + prop.ID
	// The validator is a property of the proposal, not of the reader, so it has
	// to come from an uncapped token: a capped surface is refused the queue
	// read too, which is the point — what it keeps is what a tier-zero stranger
	// keeps, filing and withdrawing its own.
	etag := liveETag(t, env, path, liveUserToken)

	status, _, body := liveDo(t, env, http.MethodGet, path, liveThirdPartyToken, "")
	require.Equal(t, 403, status, "a capped surface does not read the review queue either: %s", body)

	status, _, body = liveDoHeader(t, env, http.MethodPost, path+"/decisions", liveThirdPartyToken,
		`{"decision":"merge"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 403, status, string(body))
	require.Equal(t, "PERMISSION_REQUIRED", liveProblemCode(t, body))

	status, _, body = liveDoHeader(t, env, http.MethodPost, path+"/decisions", liveUserToken,
		`{"decision":"merge"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 201, status,
		"positive control: the same person on our own client still decides: %s", body)
}

func liveClaimETag(t *testing.T, env *liveEnv, work int64) string {
	t.Helper()
	return liveETag(t, env, "/v2/me/claims/"+idstr(work), liveUserToken)
}

func liveMoveClaim(t *testing.T, env *liveEnv, work int64, state, etag string) (int, []byte) {
	t.Helper()
	status, _, body := liveDoHeader(t, env, http.MethodPatch, "/v2/me/claims/"+idstr(work), liveUserToken,
		`{"state":"`+state+`"}`, map[string]string{"If-Match": etag})
	return status, body
}

// "c<id>.<state>" came back to itself on a round trip, so a validator taken
// before the trip still matched afterwards.
func TestLiveClaimValidatorSurvivesNoRoundTrip(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 ETag Target", 30107)

	draft := liveClaimETag(t, env, work)
	status, body := liveMoveClaim(t, env, work, "live", draft)
	require.Equal(t, 200, status, "positive control: the validator the GET hands out is the one the write takes: %s", body)

	live := liveClaimETag(t, env, work)
	require.NotEqual(t, draft, live)
	status, body = liveMoveClaim(t, env, work, "withdrawn", live)
	require.Equal(t, 200, status, string(body))

	backToDraft := liveClaimETag(t, env, work)
	require.NotEqual(t, draft, backToDraft, "the state name came back; the record did not")

	status, body = liveMoveClaim(t, env, work, "live", draft)
	require.Equal(t, 412, status, "a validator from before the round trip must not match: %s", body)

	status, body = liveMoveClaim(t, env, work, "live", backToDraft)
	require.Equal(t, 200, status, "positive control: the current validator still moves it: %s", body)
}

func TestLiveClaimNotModifiedTracksDisplayName(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W2 Cond Target", 30108)
	path := "/v2/me/claims/" + idstr(work)
	etag := liveClaimETag(t, env, work)

	status, _, body := liveDoHeader(t, env, http.MethodGet, path, liveUserToken, "",
		map[string]string{"If-None-Match": etag})
	require.Equal(t, 304, status, "positive control: an unchanged claim is still 304: %s", body)

	require.NoError(t, env.db.Model(&model.CatalogWork{}).
		Where("id = ?", work).Update("display_name", "W2 Cond Renamed").Error)

	status, _, body = liveDoHeader(t, env, http.MethodGet, path, liveUserToken, "",
		map[string]string{"If-None-Match": etag})
	require.Equal(t, 200, status, "the body moved, so the validator must have: %s", body)
	require.Contains(t, string(body), "W2 Cond Renamed")
}

// Two amendments inside one second are ordinary; at whole-second granularity
// the validator from before the first still matched after the second.
func TestProposalETagIsMicrosecondGranular(t *testing.T) {
	base := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	first := proposalETag(&editing.Proposal{ID: 7, UpdatedAt: base})
	second := proposalETag(&editing.Proposal{ID: 7, UpdatedAt: base.Add(time.Millisecond)})
	require.NotEqual(t, first, second)
	require.Equal(t, first, proposalETag(&editing.Proposal{ID: 7, UpdatedAt: base}),
		"positive control: the same instant is the same validator")
}
