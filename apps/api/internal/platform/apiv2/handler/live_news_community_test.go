package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	newsmodel "api/internal/platform/news/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"

	"github.com/stretchr/testify/require"
)

type communityNewsView struct {
	newsBodyView
	Body         string  `json:"body"`
	SubmitterUID *string `json:"submitter_uid"`
}

func TestLiveNewsAnyAccountSubmitsToCommunity(t *testing.T) {
	env := liveCatalog(t)

	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/news", livePlainToken,
		`{"title":"原创情报","summary":"一句话摘要","body":"# 标题\n\n正文"}`)
	require.Equal(t, 201, status, string(raw))
	var rec communityNewsView
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, newsmodel.SourceKeyCommunity, rec.Source.Name, "source defaults to community")
	require.Equal(t, "pending", rec.Status, "a community submission always waits for a moderator")
	require.Equal(t, "# 标题\n\n正文", rec.Body)
	require.Equal(t, "", rec.SourceURL)
	require.NotNil(t, rec.SubmitterUID)
	require.Equal(t, strconv.FormatInt(livePlainUID, 10), *rec.SubmitterUID)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/news/"+rec.ID, liveUserToken, "")
	require.Equal(t, 404, status, "another account cannot read the submission: "+string(raw))
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/news/"+rec.ID, "", "")
	require.Equal(t, 404, status, "a pending item is not public: "+string(raw))

	id, ok := repr.ParseID(rec.ID)
	require.True(t, ok)
	require.NoError(t, env.db.Exec(`UPDATE news_item SET status = ? WHERE id = ?`, newsmodel.StatusPublished, id).Error)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/news/"+rec.ID, "", "")
	require.Equal(t, 200, status, string(raw))
	var item map[string]any
	require.NoError(t, json.Unmarshal(raw, &item))
	require.Equal(t, "# 标题\n\n正文", item["body"])
	require.Equal(t, true, item["has_body"])
	require.Equal(t, strconv.FormatInt(livePlainUID, 10), item["submitter_uid"])

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/news?limit=100", "", "")
	require.Equal(t, 200, status, string(raw))
	var feed struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &feed))
	var listed map[string]any
	for _, it := range feed.Items {
		if it["id"] == rec.ID {
			listed = it
		}
	}
	require.NotNil(t, listed, "the published submission is in the feed")
	require.Equal(t, true, listed["has_body"])
	_, carriesBody := listed["body"]
	require.False(t, carriesBody, "the list never carries the body")
}

func TestLiveNewsBodyAndLinkRulesFollowTheSource(t *testing.T) {
	env := liveCatalog(t)

	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"t","summary":"s","source_url":"https://example.test/x","body":"full article"}`)
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, "/body", liveProblem(t, raw).Errors[0].Pointer, "a partner source carries no body")

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"t","summary":"s"}`)
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, problem.ReasonRequired, liveProblem(t, raw).Errors[0].Reason, "a partner item needs its link")

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", livePlainToken,
		`{"title":"t","summary":"s","source_url":"ftp://example.test/x"}`)
	require.Equal(t, 422, status, "an optional link must still be http(s): "+string(raw))

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", livePlainToken,
		`{"title":"t","summary":"s","body":"`+strings.Repeat("字", newsmodel.BodyMaxRunes+1)+`"}`)
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, problem.ReasonTooLong, liveProblem(t, raw).Errors[0].Reason)
}

func TestLiveNewsCommunityQuota(t *testing.T) {
	env := liveCatalog(t)
	var used int64
	require.NoError(t, env.db.Raw(`SELECT count(*) FROM news_item WHERE submitter_uid = ? AND source_key = ?`,
		liveSecondPlainUID, newsmodel.SourceKeyCommunity).Scan(&used).Error)
	settings.Override(t, keys.CatalogNewsSubmissionsPerDay, used+2)

	for range 2 {
		status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/news", liveSecondPlainToken,
			`{"title":"t","summary":"s"}`)
		require.Equal(t, 201, status, string(raw))
	}
	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/news", liveSecondPlainToken,
		`{"title":"t","summary":"s"}`)
	require.Equal(t, http.StatusTooManyRequests, status, string(raw))
	require.Equal(t, problem.CodeQuotaExceeded, liveProblem(t, raw).Code)

	settings.Override(t, keys.CatalogNewsSubmissionsPerDay, int64(0))
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"title":"t","summary":"s"}`)
	require.Equal(t, http.StatusTooManyRequests, status, "0 closes community submission: "+string(raw))
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"t","summary":"s","source_url":"https://example.test/x"}`)
	require.Equal(t, 201, status, "a partner source is not under the community quota: "+string(raw))
}
