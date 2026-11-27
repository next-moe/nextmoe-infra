package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

type liveHistoryPage struct {
	Items []struct {
		Object    string `json:"object"`
		WorkID    string `json:"work_id"`
		ViewCount int64  `json:"view_count"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Total      *int64  `json:"total"`
}

func liveHistory(t *testing.T, env *liveEnv, token, query string) liveHistoryPage {
	t.Helper()
	code, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/history"+query, token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var page liveHistoryPage
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	return page
}

func livePatchPrivacy(t *testing.T, env *liveEnv, token, body string) (int, map[string]any, []byte) {
	t.Helper()
	code, _, raw := liveDo(t, env, http.MethodPatch, "/v2/me/privacy", token, body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return code, out, raw
}

func TestLiveBrowsingHistoryLifecycle(t *testing.T) {
	env := liveCatalog(t)
	require.NoError(t, env.db.Exec("DELETE FROM catalog_user_work_view WHERE actor_uid = ?", liveUID).Error)
	require.NoError(t, env.db.Exec("DELETE FROM catalog_user_privacy WHERE actor_uid = ?", liveUID).Error)
	t.Cleanup(func() {
		env.db.Exec("DELETE FROM catalog_user_work_view WHERE actor_uid = ?", liveUID)
		env.db.Exec("DELETE FROM catalog_user_privacy WHERE actor_uid = ?", liveUID)
	})
	work := strconv.FormatInt(env.fx.Work, 10)
	viewPath := "/v2/me/history/works/" + work

	code, _, raw := liveDo(t, env, http.MethodPut, viewPath, liveUserToken, "")
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, _, raw = liveDo(t, env, http.MethodPut, viewPath, liveThirdPartyToken, "")
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, _, raw = liveDo(t, env, http.MethodPut, "/v2/me/history/works/999999999", liveUserToken, "")
	require.Equal(t, http.StatusNotFound, code, string(raw))

	page := liveHistory(t, env, liveUserToken, "?include_total=true")
	require.Len(t, page.Items, 1)
	require.Equal(t, "work_view", page.Items[0].Object)
	require.Equal(t, work, page.Items[0].WorkID)
	require.EqualValues(t, 1, page.Items[0].ViewCount, "scope=app sees only this app's visit")
	require.EqualValues(t, 1, *page.Total)

	code, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/history?scope=all", liveUserToken, "")
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.Equal(t, problem.CodeScopeRequired, liveProblemCode(t, raw))
	code, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/history?scope=all", liveHistoryReadToken, "")
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.Equal(t, problem.CodeBrowsingHistoryNotShared, liveProblemCode(t, raw))

	code, _, raw = livePatchPrivacy(t, env, liveThirdPartyToken, `{"browsing_history_sharing":"shared"}`)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	code, body, raw := livePatchPrivacy(t, env, liveUserToken, `{"browsing_history_sharing":"shared"}`)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "shared", body["browsing_history_sharing"])
	require.Equal(t, "on", body["browsing_history"])

	all := liveHistory(t, env, liveHistoryReadToken, "?scope=all")
	require.Len(t, all.Items, 1)
	require.EqualValues(t, 2, all.Items[0].ViewCount, "scope=all merges both apps' visits")

	sfwOnly := liveHistory(t, env, liveUserToken, "?content_limit=nsfw")
	require.Empty(t, sfwOnly.Items, "the fixture work is all-ages")

	code, _, raw = liveDo(t, env, http.MethodDelete, viewPath, liveUserToken, "")
	require.Equal(t, http.StatusNoContent, code, string(raw))
	require.Empty(t, liveHistory(t, env, liveUserToken, "").Items)
	require.Len(t, liveHistory(t, env, liveHistoryReadToken, "?scope=all").Items, 1, "the third-party app's entry stays")

	code, body, raw = livePatchPrivacy(t, env, liveThirdPartyToken, `{"browsing_history":"off"}`)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "off", body["browsing_history"])
	var n int64
	require.NoError(t, env.db.Model(&model.CatalogUserWorkView{}).Where("actor_uid = ?", liveUID).Count(&n).Error)
	require.Zero(t, n, "off erases every app's history")

	code, _, raw = liveDo(t, env, http.MethodPut, viewPath, liveUserToken, "")
	require.Equal(t, http.StatusNoContent, code, string(raw))
	require.NoError(t, env.db.Model(&model.CatalogUserWorkView{}).Where("actor_uid = ?", liveUID).Count(&n).Error)
	require.Zero(t, n, "nothing is stored while off")

	code, _, raw = livePatchPrivacy(t, env, liveThirdPartyToken, `{"browsing_history":"on"}`)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	code, body, raw = livePatchPrivacy(t, env, liveUserToken, `{"browsing_history":"on"}`)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "on", body["browsing_history"])

	code, _, raw = liveDo(t, env, http.MethodPut, viewPath, liveUserToken, "")
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, _, raw = liveDo(t, env, http.MethodDelete, "/v2/me/history?scope=all", liveUserToken, "")
	require.Equal(t, http.StatusNoContent, code, string(raw))
	require.Empty(t, liveHistory(t, env, liveUserToken, "").Items)

	code, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/history?scope=everyone", liveUserToken, "")
	require.Equal(t, http.StatusBadRequest, code, string(raw))
}

type liveHistoryChangesPage struct {
	Items []struct {
		Object          string `json:"object"`
		UserID          string `json:"user_id"`
		BrowsingHistory string `json:"browsing_history"`
		ClearedAt       string `json:"cleared_at"`
		ChangedAt       string `json:"changed_at"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func TestLiveBrowsingHistoryChangesFeed(t *testing.T) {
	env := liveCatalog(t)
	reset := func() {
		env.db.Exec("DELETE FROM catalog_user_history_control")
		env.db.Exec("DELETE FROM catalog_user_privacy WHERE actor_uid = ?", liveUID)
	}
	reset()
	t.Cleanup(reset)

	for _, tc := range []struct {
		name, token string
		want        int
		code        string
	}{
		{"no credential", "", http.StatusUnauthorized, problem.CodeMissingCredential},
		{"user token", liveUserToken, http.StatusForbidden, problem.CodeScopeRequired},
		{"key without the operator scope", liveAppKey, http.StatusForbidden, problem.CodeScopeRequired},
	} {
		code, _, raw := liveDo(t, env, http.MethodGet, historyChangesPath, tc.token, "")
		require.Equal(t, tc.want, code, "%s: %s", tc.name, raw)
		require.Equal(t, tc.code, liveProblemCode(t, raw), tc.name)
	}

	code, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/privacy", liveUserToken, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var privacy map[string]any
	require.NoError(t, json.Unmarshal(raw, &privacy))
	require.Contains(t, privacy, "browsing_history_cleared_at")
	require.Nil(t, privacy["browsing_history_cleared_at"])

	code, body, raw := livePatchPrivacy(t, env, liveThirdPartyToken, `{"browsing_history":"off"}`)
	require.Equal(t, http.StatusOK, code, string(raw))
	clearedAt, _ := body["browsing_history_cleared_at"].(string)
	require.NotEmpty(t, clearedAt, "off moves cleared_at")

	feed := func(query string) liveHistoryChangesPage {
		t.Helper()
		code, _, raw := liveDo(t, env, http.MethodGet, historyChangesPath+query, liveAppKeyFeed, "")
		require.Equal(t, http.StatusOK, code, string(raw))
		var page liveHistoryChangesPage
		require.NoError(t, json.Unmarshal(raw, &page), string(raw))
		return page
	}
	require.Empty(t, feed("").Items, "a change surfaces only after the settle window")
	require.NoError(t, env.db.Exec(`UPDATE catalog_user_history_control SET changed_at = changed_at - interval '1 minute'`).Error)

	page := feed("")
	require.Len(t, page.Items, 1)
	require.Equal(t, "browsing_history_change", page.Items[0].Object)
	require.Equal(t, strconv.FormatInt(liveUID, 10), page.Items[0].UserID)
	require.Equal(t, "off", page.Items[0].BrowsingHistory)
	require.Equal(t, clearedAt, page.Items[0].ClearedAt, "the feed and /v2/me/privacy name the same instant")
	require.Nil(t, page.NextCursor, "a partial page carries no cursor")

	full := feed("?limit=1")
	require.Len(t, full.Items, 1)
	require.NotNil(t, full.NextCursor)
	require.Empty(t, feed("?limit=1&cursor="+*full.NextCursor).Items)

	code, _, raw = liveDo(t, env, http.MethodGet, historyChangesPath+"?limit=101", liveAppKeyFeed, "")
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	code, _, raw = liveDo(t, env, http.MethodGet, historyChangesPath+"?cursor=nonsense", liveAppKeyFeed, "")
	require.Equal(t, http.StatusBadRequest, code, string(raw))
}
