package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"

	"github.com/stretchr/testify/require"
)

type newsBannerView struct {
	ID     string `json:"id"`
	Banner *struct {
		URL  string `json:"url"`
		Hash string `json:"hash"`
	} `json:"banner"`
}

func liveUploadNewsImage(t *testing.T, env *liveEnv, token string) (int, http.Header, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "banner.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("\x89PNG\r\n\x1a\n fake bytes"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/v2/me/news-images", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := env.app.Test(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, raw
}

func TestLiveNewsImageUpload(t *testing.T) {
	env := liveCatalog(t)

	require.Nil(t, env.cat.NewsImages)
	status, _, raw := liveUploadNewsImage(t, env, livePlainToken)
	require.Equal(t, 503, status, string(raw))

	store := &fakeNewsImages{}
	env.cat.NewsImages = store
	defer func() { env.cat.NewsImages = nil }()

	status, _, raw = liveUploadNewsImage(t, env, "")
	require.Equal(t, 401, status, string(raw))

	status, header, raw := liveUploadNewsImage(t, env, livePlainToken)
	require.Equal(t, 201, status, "an account with no site binding and no edit scope uploads: "+string(raw))
	var rec struct {
		Object string `json:"object"`
		URL    string `json:"url"`
		Hash   string `json:"hash"`
		Width  *int   `json:"width"`
		Height *int   `json:"height"`
	}
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "news_image", rec.Object)
	require.Equal(t, rec.URL, header.Get("Location"))
	require.Equal(t, 1280, *rec.Width)
	require.Equal(t, "news_banner", store.preset)
	require.Equal(t, "banner.png", store.filename)
	require.Equal(t, idstr(livePlainUID), store.sub)

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", livePlainToken,
		`{"title":"带封面","summary":"一句话摘要","banner_hash":"`+rec.Hash+`"}`)
	require.Equal(t, 201, status, "the hash this face returned is the one a submission carries: "+string(raw))
	var item newsBannerView
	require.NoError(t, json.Unmarshal(raw, &item))
	require.NotNil(t, item.Banner)
	require.Equal(t, rec.Hash, item.Banner.Hash)
}

func TestLiveNewsBannerHashIsVerified(t *testing.T) {
	env := liveCatalog(t)
	held := strings.Repeat("a", 64)
	foreign := strings.Repeat("c", 64)
	post := func(hash string) (int, []byte) {
		status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
			`{"source":"`+liveNewsSource+`","title":"Banner","summary":"A lede","source_url":"https://example.test/b","banner_hash":"`+hash+`"}`)
		return status, raw
	}

	require.Nil(t, env.cat.NewsImages)
	status, raw := post(held)
	require.Equal(t, 503, status, "with no image leg a banner cannot be verified, so it is not stored: "+string(raw))
	plain := liveSubmit(t, env, "No banner")
	require.NotEmpty(t, plain.ID, "a submission without a banner does not need the image leg")

	store := &fakeNewsImages{known: map[string]bool{held: true}}
	env.cat.NewsImages = store
	defer func() { env.cat.NewsImages = nil }()

	status, raw = post(foreign)
	require.Equal(t, 422, status, string(raw))
	p := liveProblem(t, raw)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	require.Len(t, p.Errors, 1)
	require.Equal(t, "/banner_hash", p.Errors[0].Pointer)
	require.Equal(t, problem.ReasonUnknownReference, p.Errors[0].Reason)

	status, raw = post(held)
	require.Equal(t, 201, status, string(raw))
	var item newsBannerView
	require.NoError(t, json.Unmarshal(raw, &item))
	require.NotNil(t, item.Banner)
	require.Equal(t, held, item.Banner.Hash)
	path := "/v2/me/news/" + item.ID

	status, _, raw = liveDo(t, env, http.MethodPatch, path, liveUserToken, `{"banner_hash":"`+foreign+`"}`)
	require.Equal(t, 422, status, string(raw))
	p = liveProblem(t, raw)
	require.Equal(t, "/banner_hash", p.Errors[0].Pointer)
	require.Equal(t, problem.ReasonUnknownReference, p.Errors[0].Reason)

	pings := store.pings
	store.pingErr = io.ErrUnexpectedEOF
	status, _, raw = liveDo(t, env, http.MethodPatch, path, liveUserToken,
		`{"title":"Banner, retitled","banner_hash":"`+held+`"}`)
	require.Equal(t, 200, status, "a resent, unchanged banner must not tie a title edit to the image service: "+string(raw))
	require.Equal(t, pings, store.pings, "an unchanged banner is not checked again")
	store.pingErr = nil

	status, _, raw = liveDo(t, env, http.MethodPatch, path, liveUserToken, `{"banner_hash":""}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &item))
	require.Nil(t, item.Banner, "the empty string still clears the banner")
}
