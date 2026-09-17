package image_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func basicAuth(id, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))
}

func uploadRequest(t *testing.T, presetName string, body []byte, auth string) *http.Request {
	t.Helper()
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	part, err := mw.CreateFormFile("file", "test.png")
	require.NoError(t, err)
	_, err = part.Write(body)
	require.NoError(t, err)
	require.NoError(t, mw.WriteField("preset", presetName))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/image/upload", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return req
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func decodeEnvelope(t *testing.T, resp *http.Response) envelope {
	t.Helper()
	var e envelope
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &e), "raw body: %s", string(body))
	return e
}

type uploadResultPayload struct {
	Hash         string            `json:"hash"`
	URL          string            `json:"url"`
	VariantURLs  map[string]string `json:"variant_urls"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	SizeBytes    int64             `json:"size_bytes"`
	Deduplicated bool              `json:"deduplicated"`
}

func callUpload(t *testing.T, body []byte, presetName, clientID, secret string) (int, *uploadResultPayload, envelope) {
	t.Helper()
	req := uploadRequest(t, presetName, body, basicAuth(clientID, secret))
	resp, err := testApp.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	defer resp.Body.Close()

	env := decodeEnvelope(t, resp)
	if resp.StatusCode != 200 || env.Code != 0 {
		return resp.StatusCode, nil, env
	}
	var payload uploadResultPayload
	require.NoError(t, json.Unmarshal(env.Data, &payload))
	return resp.StatusCode, &payload, env
}

func TestHTTP_Healthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), `"status":"ok"`)
}

func TestHTTP_Upload_Success(t *testing.T) {
	body := fixturePNG(300, 300, 50, 100, 150)
	status, result, env := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.Equal(t, 200, status, "envelope: %+v", env)
	require.NotNil(t, result)
	assert.Len(t, result.Hash, 64)
	assert.Contains(t, result.URL, result.Hash)
	assert.Equal(t, "image/webp", "image/webp")
	assert.True(t, result.Width > 0)
	assert.Contains(t, result.VariantURLs, "256")
	assert.Contains(t, result.VariantURLs, "100")
}

func TestHTTP_Upload_NoAuth_401(t *testing.T) {
	body := fixturePNG(100, 100, 0, 0, 0)
	req := uploadRequest(t, "avatar", body, "")
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 401, resp.StatusCode)
}

func TestHTTP_Upload_BadSecret_401(t *testing.T) {
	body := fixturePNG(100, 100, 0, 0, 0)
	req := uploadRequest(t, "avatar", body, basicAuth(testClientID, "wrong-secret"))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 401, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	assert.Equal(t, 80003, env.Code)
}

func TestHTTP_Upload_DisabledClient_403(t *testing.T) {
	body := fixturePNG(100, 100, 0, 0, 0)
	req := uploadRequest(t, "avatar", body, basicAuth(testDisabledClientID, "secret"))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 403, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	assert.Equal(t, 80004, env.Code)
}

func TestHTTP_Upload_DeniedPreset_403(t *testing.T) {
	body := fixturePNG(100, 100, 0, 0, 0)
	req := uploadRequest(t, "avatar", body, basicAuth(testRestrictedClient, "secret"))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 403, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	assert.Equal(t, 80006, env.Code)
}

func TestHTTP_Upload_FileTooLarge_413(t *testing.T) {
	body := fixturePNG(100, 100, 0, 0, 0)
	require.Greater(t, len(body), 128, "fixture must exceed tiny limit")
	req := uploadRequest(t, "avatar", body, basicAuth(testTinyClient, "secret"))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 413, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	assert.Equal(t, 80007, env.Code)
}

func TestHTTP_Upload_NoPreset_400(t *testing.T) {
	body := fixturePNG(100, 100, 0, 0, 0)
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	part, _ := mw.CreateFormFile("file", "test.png")
	_, _ = part.Write(body)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/image/upload", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))

	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
}

func TestHTTP_Upload_NoFile_400(t *testing.T) {
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	_ = mw.WriteField("preset", "avatar")
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/image/upload", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))

	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
}

func TestHTTP_Upload_NonImage_400(t *testing.T) {
	junk := []byte("Not an image at all — this is plain ASCII text that the MIME sniffer will reject")
	req := uploadRequest(t, "avatar", junk, basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	assert.Equal(t, 80009, env.Code)
}

func TestHTTP_Upload_UndecodableJPEG_400(t *testing.T) {
	body := append([]byte{0xff, 0xd8, 0xff, 0xdb}, "sniffed as a JPEG, refused by the decoder"...)
	req := uploadRequest(t, "avatar", body, basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	assert.Equal(t, 80010, env.Code)
}

func TestHTTP_Meta_Found(t *testing.T) {
	body := fixturePNG(220, 220, 7, 200, 50)
	_, result, _ := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.NotNil(t, result)

	req := httptest.NewRequest(http.MethodGet, "/image/"+result.Hash, nil)
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	require.Equal(t, 0, env.Code)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(env.Data, &meta))
	assert.Equal(t, result.Hash, meta["hash"])
	assert.Equal(t, "approved", meta["review_status"])
	assert.Equal(t, "image/webp", meta["mime"])
	assert.Contains(t, meta, "variant_urls")
	assert.Contains(t, meta, "sites")
}

func TestHTTP_Meta_NotFound_404(t *testing.T) {
	missing := strings.Repeat("0", 64)
	req := httptest.NewRequest(http.MethodGet, "/image/"+missing, nil)
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 404, resp.StatusCode)
}

func TestHTTP_Meta_BadHashFormat_400(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/image/short-hash", nil)
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
}

func metaBatch(t *testing.T, hashes ...string) map[string]map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"hashes": hashes})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/image/meta-batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	require.Equal(t, 0, env.Code)
	var data struct {
		Metas map[string]map[string]any `json:"metas"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &data))
	return data.Metas
}

func TestHTTP_MetaBatch_CarriesMachineSexualGrade(t *testing.T) {
	_, graded, _ := callUpload(t, fixturePNG(240, 180, 3, 60, 90), "avatar", testClientID, testClientSecret)
	require.NotNil(t, graded)
	_, ungraded, _ := callUpload(t, fixturePNG(180, 240, 90, 60, 3), "avatar", testClientID, testClientSecret)
	require.NotNil(t, ungraded)

	require.NoError(t, testDB.Exec(
		`UPDATE images SET review_labels = '{"grade": {"provider": "cloudflare-workers-ai", "level": 3}}'::jsonb
		 WHERE hash = ?`, graded.Hash).Error)
	require.NoError(t, testDB.Exec(
		`UPDATE images SET review_labels = NULL WHERE hash = ?`, ungraded.Hash).Error)

	metas := metaBatch(t, graded.Hash, ungraded.Hash)
	require.Len(t, metas, 2)

	assert.EqualValues(t, 240, metas[graded.Hash]["width"])
	assert.EqualValues(t, 180, metas[graded.Hash]["height"])
	assert.EqualValues(t, 2, metas[graded.Hash]["sexual"], "level 3 folds onto the top public value")

	require.Contains(t, metas, ungraded.Hash)
	assert.NotContains(t, metas[ungraded.Hash], "sexual",
		"an ungraded image must not read as assessed-safe")
}

func TestHTTP_MetaBatch_SafeGradeIsZeroNotAbsent(t *testing.T) {
	_, safe, _ := callUpload(t, fixturePNG(200, 200, 11, 22, 33), "avatar", testClientID, testClientSecret)
	require.NotNil(t, safe)
	require.NoError(t, testDB.Exec(
		`UPDATE images SET review_labels = '{"grade": {"level": 0}}'::jsonb WHERE hash = ?`, safe.Hash).Error)

	metas := metaBatch(t, safe.Hash)
	require.Contains(t, metas[safe.Hash], "sexual")
	assert.EqualValues(t, 0, metas[safe.Hash]["sexual"])
}

func softDelete(t *testing.T, hash, clientID, secret string) (int, envelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/image/"+hash, nil)
	req.Header.Set("Authorization", basicAuth(clientID, secret))
	resp, err := testApp.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode, decodeEnvelope(t, resp)
}

func metaStatus(t *testing.T, hash, clientID, secret string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/image/"+hash, nil)
	req.Header.Set("Authorization", basicAuth(clientID, secret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestHTTP_SoftDelete_ThenResurrectOnReupload(t *testing.T) {
	body := fixturePNG(137, 211, 211, 17, 88)

	status, result, env := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.Equal(t, 200, status, "envelope: %+v", env)
	require.NotNil(t, result)
	hash := result.Hash
	require.Equal(t, 200, metaStatus(t, hash, testClientID, testClientSecret))

	delStatus, delEnv := softDelete(t, hash, testClientID, testClientSecret)
	require.Equal(t, 200, delStatus, "soft-delete must not nil-panic; envelope: %+v", delEnv)
	require.Equal(t, 0, delEnv.Code)
	assert.Equal(t, 404, metaStatus(t, hash, testClientID, testClientSecret),
		"soft-deleted image should be hidden")

	status2, result2, env2 := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.Equal(t, 200, status2, "re-upload of a soft-deleted hash must not 500; envelope: %+v", env2)
	require.NotNil(t, result2)
	assert.Equal(t, hash, result2.Hash)
	assert.True(t, result2.Deduplicated, "resurrected upload should be a dedup hit")
	assert.Equal(t, 200, metaStatus(t, hash, testClientID, testClientSecret),
		"resurrected image should be visible again")
}

func TestHTTP_SoftDelete_OtherSite_404(t *testing.T) {
	body := fixturePNG(141, 99, 3, 240, 120)
	status, result, env := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.Equal(t, 200, status, "envelope: %+v", env)
	require.NotNil(t, result)

	delStatus, _ := softDelete(t, result.Hash, testRestrictedClient, "secret")
	assert.Equal(t, 404, delStatus, "a site that never used the hash must not soft-delete it")
	assert.Equal(t, 200, metaStatus(t, result.Hash, testClientID, testClientSecret))
}

func TestHTTP_ReferencePing_Updates(t *testing.T) {
	body := fixturePNG(180, 180, 11, 22, 33)
	_, result, _ := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.NotNil(t, result)

	req := makeJSONRequest(t,
		"/image/reference-ping",
		map[string]any{"hashes": []string{result.Hash, strings.Repeat("a", 64)}},
		basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	var data struct {
		Updated  int      `json:"updated"`
		NotFound []string `json:"not_found"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &data))
	assert.Equal(t, 1, data.Updated)
	assert.Len(t, data.NotFound, 1)
}

func TestHTTP_ReferencePing_TooMany_400(t *testing.T) {
	hashes := make([]string, 1001)
	for i := range hashes {
		hashes[i] = strings.Repeat("0", 64)
	}
	req := makeJSONRequest(t,
		"/image/reference-ping",
		map[string]any{"hashes": hashes},
		basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
}

func TestHTTP_ReferencePing_EmptyHashes(t *testing.T) {
	req := makeJSONRequest(t,
		"/image/reference-ping",
		map[string]any{"hashes": []string{}},
		basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
}

func TestHTTP_Stats(t *testing.T) {
	body := fixturePNG(150, 150, 1, 2, 3)
	_, _, _ = callUpload(t, body, "avatar", testClientID, testClientSecret)

	req := httptest.NewRequest(http.MethodGet, "/image/stats", nil)
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))
	resp, err := testApp.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	env := decodeEnvelope(t, resp)
	require.Equal(t, 0, env.Code)
	var stats struct {
		UploadCount       int   `json:"upload_count"`
		UniqueImages      int   `json:"unique_images"`
		DeduplicatedCount int   `json:"deduplicated_count"`
		TotalBytes        int64 `json:"total_bytes"`
	}
	require.NoError(t, json.Unmarshal(env.Data, &stats))
	assert.GreaterOrEqual(t, stats.UploadCount, 1)
	assert.GreaterOrEqual(t, stats.UniqueImages, 1)
}

func TestHTTP_Metrics_Exposes(t *testing.T) {
	body := fixturePNG(80, 80, 9, 9, 9)
	_, _, _ = callUpload(t, body, "avatar", testClientID, testClientSecret)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp, err := testApp.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	body2, _ := io.ReadAll(resp.Body)
	text := string(body2)
	assert.Contains(t, text, "image_upload_total")
	assert.Contains(t, text, "image_upload_duration_seconds")
	assert.Contains(t, text, "go_goroutines")
}

func TestHTTP_DedupAcrossClients(t *testing.T) {
	body := fixturePNG(190, 190, 222, 111, 33)
	_, r1, _ := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.NotNil(t, r1)
	assert.False(t, r1.Deduplicated)

	_, r2, _ := callUpload(t, body, "avatar", testClientID, testClientSecret)
	require.NotNil(t, r2)
	assert.True(t, r2.Deduplicated)
	assert.Equal(t, r1.Hash, r2.Hash)
	assert.Equal(t, r1.URL, r2.URL)
}

func makeJSONRequest(t *testing.T, path string, body any, auth string) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return req
}

var _ = fmt.Sprintf
