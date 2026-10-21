package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

type liveDiscussionPost struct {
	ID             string   `json:"id"`
	AuthorUID      string   `json:"author_uid"`
	AuthorReviewer bool     `json:"author_reviewer"`
	State          string   `json:"state"`
	Body           string   `json:"body"`
	ReplyToID      *string  `json:"reply_to_id"`
	MentionUIDs    []string `json:"mention_uids"`
	ModerationNote *string  `json:"moderation_note"`
	EditedAt       *string  `json:"edited_at"`
	Author         *struct {
		ID string `json:"id"`
	} `json:"author"`
}

type liveDiscussionPage struct {
	Items    []liveDiscussionPost `json:"items"`
	Total    int64                `json:"total"`
	OwnerUID *string              `json:"owner_uid"`
	Viewer   *struct {
		CanPost     bool `json:"can_post"`
		CanModerate bool `json:"can_moderate"`
		Watching    bool `json:"watching"`
		Muted       bool `json:"muted"`
	} `json:"viewer"`
}

type liveDiscussionNotice struct {
	Reason        string             `json:"reason"`
	ViewerIsOwner bool               `json:"viewer_is_owner"`
	Post          liveDiscussionPost `json:"post"`
}

func liveDiscussionThread(t *testing.T, env *liveEnv, kind string, id int64, token string) (int, liveDiscussionPage, []byte) {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, fmt.Sprintf("/v2/catalog/discussions/%s/%d", kind, id), token, "")
	var page liveDiscussionPage
	if status == http.StatusOK {
		require.NoError(t, json.Unmarshal(body, &page), string(body))
	}
	return status, page, body
}

func livePost(t *testing.T, env *liveEnv, token, payload string) (int, liveDiscussionPost, []byte) {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodPost, "/v2/me/discussions", token, payload)
	var post liveDiscussionPost
	if status == http.StatusCreated {
		require.NoError(t, json.Unmarshal(body, &post), string(body))
	}
	return status, post, body
}

func liveNotices(t *testing.T, env *liveEnv, token string, since time.Time) []liveDiscussionNotice {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/me/discussion-notices?since="+since.UTC().Format(time.RFC3339Nano), token, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var page struct {
		Items []liveDiscussionNotice `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page), string(body))
	return page.Items
}

func liveForgetProposals(env *liveEnv, ids ...int64) {
	env.db.Exec(`DELETE FROM edit_discussion_post WHERE anchor_kind = 'proposal' AND anchor_id IN ?`, ids)
	env.db.Exec(`DELETE FROM edit_discussion_watcher WHERE anchor_kind = 'proposal' AND anchor_id IN ?`, ids)
	env.db.Exec(`DELETE FROM edit_proposal WHERE id IN ?`, ids)
}

func TestLiveDiscussionOnAProposal(t *testing.T) {
	env := liveCatalog(t)
	work := liveClaimedBy(t, env, "Discussion Probe", 39701, livePlainUID)
	prop := liveProposalOnSite(t, env, liveSite, work, "Discussion Probe Renamed")
	other := liveProposalOnSite(t, env, liveSite, work, "Another Rename")
	t.Cleanup(func() { liveForgetProposals(env, prop.ID, other.ID) })
	since := time.Now().Add(-time.Minute)

	status, page, body := liveDiscussionThread(t, env, "proposal", prop.ID, "")
	require.Equal(t, http.StatusOK, status, "a proposal's thread is public like the proposal: %s", body)
	require.Empty(t, page.Items)
	require.Nil(t, page.Viewer, "an anonymous read carries no viewer block")
	require.NotNil(t, page.OwnerUID)
	require.Equal(t, idstr(liveSecondPlainUID), *page.OwnerUID)

	status, first, body := livePost(t, env, liveAdminBothToken, fmt.Sprintf(
		`{"anchor_kind":"proposal","anchor_id":"%d","body":"  请补充来源  ","mention_uids":["%d","%d"]}`,
		prop.ID, liveSecondPlainUID, liveUID))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.True(t, first.AuthorReviewer, "an admin reviews work proposals")
	require.Equal(t, "请补充来源", first.Body, "the body is trimmed")
	require.Equal(t, []string{idstr(liveSecondPlainUID)}, first.MentionUIDs, "the author's own mention is dropped")
	require.NotNil(t, first.Author)

	notices := liveNotices(t, env, liveSecondBothToken, since)
	require.Len(t, notices, 1)
	require.Equal(t, "mention", notices[0].Reason)
	require.True(t, notices[0].ViewerIsOwner)
	require.Equal(t, first.ID, notices[0].Post.ID)
	require.Empty(t, liveNotices(t, env, liveAdminBothToken, since), "nobody is notified of their own post")

	status, reply, body := livePost(t, env, liveSecondBothToken, fmt.Sprintf(
		`{"anchor_kind":"proposal","anchor_id":"%d","body":"来源是官网公告","reply_to_id":"%s"}`, prop.ID, first.ID))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.False(t, reply.AuthorReviewer)
	require.NotNil(t, reply.ReplyToID)

	adminNotices := liveNotices(t, env, liveAdminBothToken, since)
	require.Len(t, adminNotices, 1, "the admin watches the thread they posted in")
	require.Equal(t, "reply", adminNotices[0].Reason)
	require.False(t, adminNotices[0].ViewerIsOwner)

	status, page, body = liveDiscussionThread(t, env, "proposal", prop.ID, liveSecondBothToken)
	require.Equal(t, http.StatusOK, status, string(body))
	require.Len(t, page.Items, 2)
	require.Equal(t, int64(2), page.Total)
	require.NotNil(t, page.Viewer)
	require.True(t, page.Viewer.CanPost)
	require.False(t, page.Viewer.CanModerate)
	require.True(t, page.Viewer.Watching)

	status, _, body = livePost(t, env, liveThirdPartyToken, fmt.Sprintf(`{"anchor_kind":"proposal","anchor_id":"%d","body":"hi"}`, prop.ID))
	require.Equal(t, http.StatusForbidden, status, string(body))
	require.Equal(t, "PERMISSION_REQUIRED", liveProblemCode(t, body))

	status, _, body = livePost(t, env, liveSecondBothToken, fmt.Sprintf(`{"anchor_kind":"proposal","anchor_id":"%d","body":"   "}`, prop.ID))
	require.Equal(t, http.StatusUnprocessableEntity, status, string(body))

	status, _, body = livePost(t, env, liveSecondBothToken, fmt.Sprintf(
		`{"anchor_kind":"proposal","anchor_id":"%d","body":"跨线程引用","reply_to_id":"%s"}`, other.ID, first.ID))
	require.Equal(t, http.StatusUnprocessableEntity, status, "reply_to must be in the same thread: %s", body)

	status, _, body = livePost(t, env, liveSecondBothToken, `{"anchor_kind":"nope","anchor_id":"1","body":"x"}`)
	require.Equal(t, http.StatusNotFound, status, string(body))

	status, _, body = liveDo(t, env, http.MethodPatch, "/v2/me/discussions/"+first.ID, liveSecondBothToken, `{"body":"篡改"}`)
	require.Equal(t, http.StatusForbidden, status, "only the author edits: %s", body)
	status, _, body = liveDo(t, env, http.MethodPatch, "/v2/me/discussions/"+first.ID, liveAdminBothToken, `{"body":"请补充来源链接"}`)
	require.Equal(t, http.StatusOK, status, string(body))
	var edited liveDiscussionPost
	require.NoError(t, json.Unmarshal(body, &edited))
	require.NotNil(t, edited.EditedAt)

	path := "/v2/moderation/discussions/" + reply.ID + "/visibility"
	status, _, body = liveDo(t, env, http.MethodPost, path, liveSecondBothToken, `{"hidden":true}`)
	require.Equal(t, http.StatusForbidden, status, "a plain user cannot moderate: %s", body)
	status, _, body = liveDo(t, env, http.MethodPost, path, liveAdminBothToken, `{"hidden":true,"note":"含联系方式"}`)
	require.Equal(t, http.StatusOK, status, string(body))

	_, page, _ = liveDiscussionThread(t, env, "proposal", prop.ID, "")
	require.Equal(t, "hidden", page.Items[1].State)
	require.Empty(t, page.Items[1].Body, "a hidden post's text stays with moderators")
	require.Nil(t, page.Items[1].ModerationNote)
	_, page, _ = liveDiscussionThread(t, env, "proposal", prop.ID, liveAdminBothToken)
	require.Equal(t, "来源是官网公告", page.Items[1].Body)
	require.NotNil(t, page.Items[1].ModerationNote)
	require.True(t, page.Viewer.CanModerate)

	status, _, body = liveDo(t, env, http.MethodPatch, "/v2/me/discussions/"+reply.ID, liveSecondBothToken, `{"body":"改一下"}`)
	require.Equal(t, http.StatusConflict, status, "a hidden post cannot be edited: %s", body)

	status, _, body = liveDo(t, env, http.MethodPut, "/v2/me/discussion-watches", liveSecondBothToken,
		fmt.Sprintf(`{"anchor_kind":"proposal","anchor_id":"%d","muted":true}`, prop.ID))
	require.Equal(t, http.StatusOK, status, string(body))
	mutedSince := time.Now()
	status, _, body = livePost(t, env, liveAdminBothToken, fmt.Sprintf(`{"anchor_kind":"proposal","anchor_id":"%d","body":"普通回复"}`, prop.ID))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.Empty(t, liveNotices(t, env, liveSecondBothToken, mutedSince), "a muted thread stays quiet")
	status, _, body = livePost(t, env, liveAdminBothToken, fmt.Sprintf(
		`{"anchor_kind":"proposal","anchor_id":"%d","body":"@提案人 请看","mention_uids":["%d"]}`, prop.ID, liveSecondPlainUID))
	require.Equal(t, http.StatusCreated, status, string(body))
	muted := liveNotices(t, env, liveSecondBothToken, mutedSince)
	require.Len(t, muted, 1, "a mention still gets through")
	require.Equal(t, "mention", muted[0].Reason)

	status, _, body = liveDo(t, env, http.MethodDelete, "/v2/me/discussions/"+first.ID, liveAdminBothToken, "")
	require.Equal(t, http.StatusNoContent, status, string(body))
	_, page, _ = liveDiscussionThread(t, env, "proposal", prop.ID, liveAdminBothToken)
	require.Equal(t, "deleted", page.Items[0].State)
	require.Empty(t, page.Items[0].Body)
	require.Equal(t, int64(3), page.Total, "deleted posts leave the count")
}

func TestLiveDiscussionOnAMergeRequestFollowsItsReaders(t *testing.T) {
	env := liveCatalog(t)
	a := liveClaimedBy(t, env, "Discussion Merge A", 39711, livePlainUID)
	b := liveClaimedBy(t, env, "Discussion Merge B", 39712, livePlainUID)
	req, err := env.cat.Merges.RequestMerge(context.Background(), model.EntityTypeWork, a, b, livePlainUID, "同一部作品")
	require.NoError(t, err)
	t.Cleanup(func() {
		env.db.Exec(`DELETE FROM edit_discussion_post WHERE anchor_kind = 'merge_request' AND anchor_id = ?`, req.ID)
		env.db.Exec(`DELETE FROM edit_discussion_watcher WHERE anchor_kind = 'merge_request' AND anchor_id = ?`, req.ID)
		env.db.Exec(`DELETE FROM catalog_merge_proposal WHERE id = ?`, req.ID)
	})

	status, _, body := liveDiscussionThread(t, env, "merge_request", req.ID, "")
	require.Equal(t, http.StatusNotFound, status, "a merge request's thread is not public: %s", body)
	status, _, body = liveDiscussionThread(t, env, "merge_request", req.ID, liveSecondBothToken)
	require.Equal(t, http.StatusNotFound, status, "another plain user cannot read it: %s", body)
	status, _, body = liveDiscussionThread(t, env, "merge_request", req.ID, livePlainBothToken)
	require.Equal(t, http.StatusOK, status, "the requester reads it: %s", body)
	status, _, body = liveDiscussionThread(t, env, "merge_request", req.ID, liveCentralBothToken)
	require.Equal(t, http.StatusOK, status, "a merge reviewer reads it: %s", body)

	since := time.Now().Add(-time.Minute)
	status, post, body := livePost(t, env, liveCentralBothToken, fmt.Sprintf(`{"anchor_kind":"merge_request","anchor_id":"%d","body":"VNDB 怎么列的?"}`, req.ID))
	require.Equal(t, http.StatusCreated, status, string(body))
	require.True(t, post.AuthorReviewer)
	notices := liveNotices(t, env, livePlainBothToken, since)
	require.Len(t, notices, 1, "the requester watches their request")
	require.True(t, notices[0].ViewerIsOwner)

	status, _, body = livePost(t, env, liveSecondBothToken, fmt.Sprintf(`{"anchor_kind":"merge_request","anchor_id":"%d","body":"我也说一句"}`, req.ID))
	require.Equal(t, http.StatusNotFound, status, "posting needs read access first: %s", body)
}
