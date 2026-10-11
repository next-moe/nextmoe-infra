package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
	"api/pkg/imageclient"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type liveClaimBody struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	State  string `json:"state"`
}

type liveRevisionBody struct {
	Object        string   `json:"object"`
	ID            string   `json:"id"`
	TargetObject  string   `json:"target_object"`
	EntityID      string   `json:"entity_id"`
	SiteWorkID    *string  `json:"site_work_id"`
	Seq           int      `json:"seq"`
	Action        string   `json:"action"`
	ChangedFields []string `json:"changed_fields"`
	ActorUID      string   `json:"actor_uid"`
	Site          string   `json:"site"`
	CreatedAt     string   `json:"created_at"`
	Diff          *[]struct {
		Key  string `json:"key"`
		From any    `json:"from"`
		To   any    `json:"to"`
	} `json:"diff"`
	DiffBase *string `json:"diff_base"`
}

type liveRevisionPage struct {
	Items      []liveRevisionBody `json:"items"`
	NextCursor *string            `json:"next_cursor"`
	Total      *int64             `json:"total"`
}

type liveProposalBody struct {
	Object       string  `json:"object"`
	ID           string  `json:"id"`
	State        string  `json:"state"`
	TargetObject string  `json:"target_object"`
	EntityID     string  `json:"entity_id"`
	ProposerUID  string  `json:"proposer_uid"`
	Site         string  `json:"site"`
	CreatedAt    string  `json:"created_at"`
	DecidedAt    *string `json:"decided_at"`
	DecisionNote *string `json:"decision_note"`
	Patch        *any    `json:"patch"`
	Amendments   *[]struct {
		Object     string `json:"object"`
		ID         string `json:"id"`
		AmenderUID string `json:"amender_uid"`
	} `json:"amendments"`
}

type liveProposalPage struct {
	Items      []liveProposalBody `json:"items"`
	NextCursor *string            `json:"next_cursor"`
	Total      *int64             `json:"total"`
}

// A claim the token's user owns, parked in draft.
func liveDraftClaim(t *testing.T, env *liveEnv, name string, product int64) int64 {
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
		ProductWorkID: &product, ActorUID: liveUID,
	})
	require.NoError(t, err)
	return w.ID
}

func liveWorkName(t *testing.T, env *liveEnv, workID int64) string {
	t.Helper()
	var name string
	require.NoError(t, env.db.Model(&model.CatalogWork{}).
		Select("display_name").Where("id = ?", workID).Scan(&name).Error)
	return name
}

func liveClaimEventCount(t *testing.T, env *liveEnv, workID int64) int64 {
	t.Helper()
	var n int64
	require.NoError(t, env.db.Model(&model.CatalogClaimEvent{}).
		Where("work_id = ?", workID).Count(&n).Error)
	return n
}

func TestLiveClaimPublishAndSubmit(t *testing.T) {
	env := liveCatalog(t)

	// draft -> live via PATCH {state: live}
	id := liveDraftClaim(t, env, "D35 Publish", 30001)
	before := liveClaimEventCount(t, env, id)
	path := "/v2/me/claims/" + idstr(id)
	etag := liveETag(t, env, path, liveUserToken)

	status, _, body := liveDo(t, env, http.MethodPatch, path, liveUserToken, `{"state":"live"}`)
	require.Equal(t, 428, status, string(body))

	status, _, body = liveDoHeader(t, env, http.MethodPatch, path, liveUserToken,
		`{"state":"live"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 200, status, string(body))
	var rec liveClaimBody
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "live", rec.State)
	require.Equal(t, before+1, liveClaimEventCount(t, env, id),
		"publish must append a catalog_claim_event row; the moemoepoint ledger reads that table")

	// live -> live is not a legal owner transition, and the detail names the targets
	etag = liveETag(t, env, path, liveUserToken)
	status, _, body = liveDoHeader(t, env, http.MethodPatch, path, liveUserToken,
		`{"state":"live"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 409, status, string(body))
	var prob struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(body, &prob))
	require.Equal(t, "INVALID_STATE_TRANSITION", prob.Code)
	require.Contains(t, prob.Detail, "live")
	require.Contains(t, prob.Detail, "withdrawn")

	// draft -> pending via PATCH {state: pending}
	sid := liveDraftClaim(t, env, "D35 Submit", 30002)
	spath := "/v2/me/claims/" + idstr(sid)
	setag := liveETag(t, env, spath, liveUserToken)
	status, _, body = liveDoHeader(t, env, http.MethodPatch, spath, liveUserToken,
		`{"state":"pending"}`, map[string]string{"If-Match": setag})
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "pending", rec.State)
}

func TestLiveClaimBanUnban(t *testing.T) {
	env := liveCatalog(t)
	id := liveDraftClaim(t, env, "D35 Ban", 30003)
	path := "/v2/me/claims/" + idstr(id)
	etag := liveETag(t, env, path, liveUserToken)
	status, _, body := liveDoHeader(t, env, http.MethodPatch, path, liveUserToken,
		`{"state":"live"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 200, status, string(body))

	modPath := "/v2/moderation/claims/" + idstr(id)
	decisions := modPath + "/decisions"
	before := liveClaimEventCount(t, env, id)

	etag = liveETag(t, env, modPath, liveUserToken)
	status, _, body = liveDoHeader(t, env, http.MethodPost, decisions, liveUserToken,
		`{"decision":"ban","note":"spam"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 201, status, string(body))

	status, _, body = liveDo(t, env, http.MethodGet, modPath, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var rec liveClaimBody
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "hidden", rec.State)

	etag = liveETag(t, env, modPath, liveUserToken)
	status, _, body = liveDoHeader(t, env, http.MethodPost, decisions, liveUserToken,
		`{"decision":"unban"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 201, status, string(body))

	status, _, body = liveDo(t, env, http.MethodGet, modPath, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "live", rec.State, "unban restores the state the claim was hidden from")
	require.Equal(t, before+2, liveClaimEventCount(t, env, id))

	// a stale validator loses to the state the decisions already moved
	status, _, body = liveDoHeader(t, env, http.MethodPost, decisions, liveUserToken,
		`{"decision":"ban","note":"again"}`, map[string]string{"If-Match": `"c` + idstr(id) + `.draft"`})
	require.Equal(t, 412, status, string(body))
}

// The live token carries admin roles, so catalog.edit.trusted makes a work-title
// edit auto-merge at creation; only an untrusted proposal is still open when the
// moderation face sees it.
func liveMergedProposal(t *testing.T, env *liveEnv, workID int64, name string) string {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodPost, "/v2/me/proposals", liveUserToken, fmt.Sprintf(
		`{"entity_type":%q,"entity_id":%q,"patch":{%q:%q},"note":"d35"}`,
		editspec.TypeWork, idstr(workID), editspec.FieldWorkDisplayName, name))
	require.Equal(t, 201, status, string(body))
	var prop liveProposalBody
	require.NoError(t, json.Unmarshal(body, &prop))
	if prop.State == "merged" {
		return prop.ID
	}
	path := "/v2/moderation/proposals/" + prop.ID
	etag := liveETag(t, env, path, liveUserToken)
	status, _, body = liveDoHeader(t, env, http.MethodPost, path+"/decisions", liveUserToken,
		`{"decision":"merge","note":"looks right"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 201, status, string(body))
	return prop.ID
}

func TestLiveModerationProposalDetail(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "D35 Proposal Target", 30004)

	status, _, body := liveDo(t, env, http.MethodPost, "/v2/me/proposals", liveUserToken, fmt.Sprintf(
		`{"entity_type":%q,"entity_id":%q,"patch":{%q:"D35 Renamed"},"note":"d35"}`,
		editspec.TypeWork, idstr(work), editspec.FieldWorkDisplayName))
	require.Equal(t, 201, status, string(body))
	var prop liveProposalBody
	require.NoError(t, json.Unmarshal(body, &prop))

	path := "/v2/moderation/proposals/" + prop.ID
	status, _, body = liveDo(t, env, http.MethodGet, path+"?include=patch,amendments", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var detail liveProposalBody
	require.NoError(t, json.Unmarshal(body, &detail))
	require.Equal(t, prop.ID, detail.ID)
	require.Equal(t, idstr(liveUID), detail.ProposerUID)
	require.Equal(t, liveSite, detail.Site)
	require.NotNil(t, detail.Patch, "the moderation face must publish the patch a decision is taken on")
	require.NotNil(t, detail.Amendments)

	// the validator the detail hands out is the one its own decision accepts
	etag := liveETag(t, env, path, liveUserToken)
	status, _, body = liveDo(t, env, http.MethodPost, path+"/decisions", liveUserToken, `{"decision":"merge"}`)
	require.Equal(t, 428, status, string(body))
	status, _, body = liveDoHeader(t, env, http.MethodPost, path+"/decisions", liveUserToken,
		`{"decision":"merge"}`, map[string]string{"If-Match": etag})
	if detail.State == "merged" {
		require.Equal(t, 409, status, string(body))
		var prob struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(body, &prob))
		require.Equal(t, "DECISION_ALREADY_MADE", prob.Code)
		return
	}
	require.Equal(t, 201, status, string(body))
}

func TestLivePublicEditHistory(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "D35 History", 30005)
	propID := liveMergedProposal(t, env, work, "D35 History Renamed")

	// 1. one entity's revision log, newest first
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/revisions?object=work&entity_id="+idstr(work)+"&include_total=true", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var page liveRevisionPage
	require.NoError(t, json.Unmarshal(body, &page))
	require.NotEmpty(t, page.Items)
	require.NotNil(t, page.Total)
	top := page.Items[0]
	require.Equal(t, "revision", top.Object)
	require.Equal(t, "work", top.TargetObject)
	require.Equal(t, idstr(work), top.EntityID)
	require.Equal(t, idstr(liveUID), top.ActorUID)
	require.Contains(t, []string{"merged", "direct"}, top.Action)
	require.Contains(t, top.ChangedFields, editspec.FieldWorkDisplayName)
	require.NotNil(t, top.SiteWorkID, "the contributor cron reads the claiming site's own work id off this face")
	require.Equal(t, "30005", *top.SiteWorkID)
	require.Equal(t, liveSite, top.Site)

	// 2. the ascending watermark shape the two crons read
	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/revisions?sort=recorded_asc&site="+liveSite+"&limit=1", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var asc liveRevisionPage
	require.NoError(t, json.Unmarshal(body, &asc))
	require.Len(t, asc.Items, 1)
	require.NotNil(t, asc.NextCursor)
	first := asc.Items[0].ID
	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/revisions?sort=recorded_asc&site="+liveSite+"&limit=1&cursor="+*asc.NextCursor, liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &asc))
	require.Len(t, asc.Items, 1)
	require.Greater(t, asc.Items[0].ID, first, "recorded_asc must walk ids upward so a watermark never skips a row")

	// 5. revision detail carries the id revert takes, plus the field-level diff
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/revisions/"+top.ID+"?include=diff", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var one liveRevisionBody
	require.NoError(t, json.Unmarshal(body, &one))
	require.Equal(t, top.ID, one.ID)
	require.NotNil(t, one.Diff)
	require.NotEmpty(t, *one.Diff, "seq=%d changed_fields=%v body=%s", one.Seq, one.ChangedFields, string(body))
	require.Equal(t, editspec.FieldWorkDisplayName, (*one.Diff)[0].Key)
	require.Equal(t, "D35 History Renamed", (*one.Diff)[0].To)

	// A second edit gives the history two points, so the revision id the detail
	// face hands out can be carried straight into a revert.
	liveMergedProposal(t, env, work, "D35 History Renamed Again")
	status, _, body = liveDoHeader(t, env, http.MethodPost, "/v2/moderation/reverts", liveUserToken,
		`{"revision_id":"`+one.ID+`","reason":"d35 revert"}`, nil)
	require.Equal(t, 201, status, string(body))
	require.Equal(t, "D35 History Renamed", liveWorkName(t, env, work))

	// without include=diff the block is absent, not empty
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/revisions/"+top.ID, liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var bare liveRevisionBody
	require.NoError(t, json.Unmarshal(body, &bare))
	require.Nil(t, bare.Diff)
	require.Nil(t, bare.DiffBase)

	// 3 + 4. proposals by proposer and state, and one proposal by id
	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/proposals?object=work&proposer_uid="+idstr(liveUID)+"&state=merged&include_total=true&limit=1",
		liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var props liveProposalPage
	require.NoError(t, json.Unmarshal(body, &props))
	require.NotNil(t, props.Total)
	require.GreaterOrEqual(t, *props.Total, int64(1))
	require.Len(t, props.Items, 1)
	require.Equal(t, "merged", props.Items[0].State)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/proposals/"+propID+"?include=amendments", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var pub liveProposalBody
	require.NoError(t, json.Unmarshal(body, &pub))
	require.Equal(t, "proposal", pub.Object)
	require.Equal(t, idstr(liveUID), pub.ProposerUID)
	require.Equal(t, "work", pub.TargetObject)
	require.Equal(t, idstr(work), pub.EntityID)
	require.NotEmpty(t, pub.CreatedAt)
	require.NotNil(t, pub.DecidedAt)
	require.NotNil(t, pub.Amendments)
	require.Nil(t, pub.Patch, "the public face must not publish the proposed payload")
	require.Nil(t, pub.DecisionNote, "the public face must not publish the reviewer's note")

	// include=patch is not in this collection's vocabulary
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/proposals/"+propID+"?include=patch", liveAppKey, "")
	require.Equal(t, 400, status, string(body))

	// like every other /v2/catalog read, the history answers without a credential too
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/revisions", "", "")
	require.Equal(t, 200, status, string(body))
}

func liveUploadImage(t *testing.T, env *liveEnv, preset string) (int, http.Header, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("preset", preset))
	part, err := mw.CreateFormFile("file", "cover.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("\x89PNG\r\n\x1a\n fake bytes"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/v2/me/edit-images", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+liveUserToken)
	resp, err := env.app.Test(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, raw
}

func TestLiveEditImageUpload(t *testing.T) {
	env := liveCatalog(t)

	require.Nil(t, env.cat.Uploads)
	status, _, body := liveUploadImage(t, env, "cover")
	require.Equal(t, 503, status, string(body))

	var gotPreset, gotSub, gotName string
	env.cat.Uploads = func(_ context.Context, r io.Reader, filename, preset, sub string) (*imageclient.UploadResult, error) {
		b, _ := io.ReadAll(r)
		require.NotEmpty(t, b, "the handler must stream the multipart part, not an exhausted reader")
		gotPreset, gotSub, gotName = preset, sub, filename
		return &imageclient.UploadResult{
			Hash: "9f3c" + strings13("a", 60), URL: "https://img.example.dev/image/9f3c",
			Width: 800, Height: 1131, Thumbhash: "3PcNNYSFeXh", SizeBytes: 4211, Deduplicated: true,
		}, nil
	}
	defer func() { env.cat.Uploads = nil }()

	status, header, body := liveUploadImage(t, env, "cover")
	require.Equal(t, 201, status, string(body))
	require.Equal(t, "https://img.example.dev/image/9f3c", header.Get("Location"))
	require.Equal(t, "catalog_cover", gotPreset, "the v2 preset token must map onto the image service's own preset")
	require.Equal(t, liveSite+":"+idstr(liveUID), gotSub)
	require.Equal(t, "cover.png", gotName)

	var rec struct {
		Object         string  `json:"object"`
		Preset         string  `json:"preset"`
		URL            string  `json:"url"`
		Hash           string  `json:"hash"`
		Width          *int    `json:"width"`
		Height         *int    `json:"height"`
		Thumbhash      *string `json:"thumbhash"`
		Sexual         *string `json:"sexual"`
		SizeBytes      int64   `json:"size_bytes"`
		IsDeduplicated bool    `json:"is_deduplicated"`
	}
	require.NoError(t, json.Unmarshal(body, &rec))
	require.Equal(t, "edit_image", rec.Object)
	require.Equal(t, "cover", rec.Preset)
	require.Equal(t, 800, *rec.Width)
	require.Equal(t, int64(4211), rec.SizeBytes)
	require.True(t, rec.IsDeduplicated)
	require.Nil(t, rec.Sexual, "an upload receipt predates any assessment")

	status, _, body = liveUploadImage(t, env, "galgame_banner")
	require.Equal(t, 422, status, string(body))
}

func TestLiveCreditNamesBatchLane(t *testing.T) {
	env := liveCatalog(t)

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/credit-names?ids="+idstr(env.fx.Credit)+",999999999", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Object string `json:"object"`
		Items  []struct {
			Object string `json:"object"`
			ID     string `json:"id"`
		} `json:"items"`
		NextCursor *string   `json:"next_cursor"`
		Total      *int64    `json:"total"`
		Missing    *[]string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1, string(body))
	require.Equal(t, idstr(env.fx.Credit), page.Items[0].ID)
	require.Nil(t, page.NextCursor, "the batch lane does not paginate")
	require.Nil(t, page.Total)
	require.NotNil(t, page.Missing, string(body))
	require.Equal(t, []string{"999999999"}, *page.Missing)
}

func strings13(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}
