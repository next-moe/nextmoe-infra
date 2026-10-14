package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"api/pkg/trustclient"

	"github.com/stretchr/testify/require"
)

type fakeRatingTrust struct {
	mu       sync.Mutex
	decision string
	err      error
	checks   []trustclient.CheckRequest
	scans    chan trustclient.ScanRequest
}

func newFakeRatingTrust() *fakeRatingTrust {
	return &fakeRatingTrust{decision: "allow", scans: make(chan trustclient.ScanRequest, 8)}
}

func (f *fakeRatingTrust) Check(_ context.Context, req trustclient.CheckRequest) (string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, req)
	if f.err != nil {
		return "", nil, f.err
	}
	return f.decision, []string{"term"}, nil
}

func (f *fakeRatingTrust) Scan(_ context.Context, req trustclient.ScanRequest) (int64, bool, error) {
	f.scans <- req
	return 1, false, nil
}

func (f *fakeRatingTrust) answer(decision string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decision, f.err = decision, err
}

func (f *fakeRatingTrust) checkCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.checks)
}

func (f *fakeRatingTrust) lastCheck() trustclient.CheckRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checks[len(f.checks)-1]
}

func (f *fakeRatingTrust) nextScan(t *testing.T) trustclient.ScanRequest {
	t.Helper()
	select {
	case s := <-f.scans:
		return s
	case <-time.After(3 * time.Second):
		t.Fatal("expected a trust scan")
		return trustclient.ScanRequest{}
	}
}

func (f *fakeRatingTrust) noScan(t *testing.T) {
	t.Helper()
	select {
	case s := <-f.scans:
		t.Fatalf("unexpected trust scan %+v", s)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestLiveThirdPartyRatingTrust(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	path := "/v2/me/ratings/" + idstr(fx.Work)
	cleanLiveRatings(t, env, liveUID)
	trust := newFakeRatingTrust()
	env.cat.RatingTrust = trust
	t.Cleanup(func() {
		env.cat.RatingTrust = nil
		cleanLiveRatings(t, env, liveUID)
	})

	put := func(token, summary string) (int, liveRating, []byte) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"overall": 8, "recommend": "yes", "short_summary": summary})
		status, _, raw := liveDo(t, env, http.MethodPut, path, token, string(body))
		var rec liveRating
		if status == 200 {
			require.NoError(t, json.Unmarshal(raw, &rec))
		}
		return status, rec, raw
	}

	status, _, raw := put(liveUserToken, "第一方写的短评")
	require.Equal(t, 200, status, string(raw))
	require.Zero(t, trust.checkCount(), "a first-party write is the site's to check")
	trust.noScan(t)

	status, rec, raw := put(liveThirdPartyToken, "第三方写的短评")
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "第三方写的短评", rec.ShortSummary)
	require.Equal(t, 1, trust.checkCount())
	chk := trust.lastCheck()
	require.Equal(t, RatingTrustSite, chk.Site)
	require.Equal(t, "第三方写的短评", chk.Text)
	require.Equal(t, liveUID, *chk.AuthorID)
	scan := trust.nextScan(t)
	require.Equal(t, trustclient.ScanRequest{
		Site: RatingTrustSite, SubjectKind: RatingTrustSubjectKind, SubjectID: rec.ID,
		Text: "第三方写的短评", AuthorID: scan.AuthorID,
	}, scan)
	require.Equal(t, liveUID, *scan.AuthorID)

	status, _, raw = put(liveThirdPartyToken, "第三方写的短评")
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, 1, trust.checkCount(), "an unchanged short review is not checked again")
	trust.noScan(t)

	status, _, raw = put(liveThirdPartyToken, "")
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, 1, trust.checkCount(), "an empty short review has nothing to check")
	trust.noScan(t)

	trust.answer("deny", nil)
	status, _, raw = put(liveThirdPartyToken, "被拒绝的短评")
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, "VALIDATION_FAILED", problemCode(t, raw))
	require.Contains(t, string(raw), `"pointer":"/short_summary"`)
	require.Contains(t, string(raw), `"NOT_ALLOWED_VALUE"`)
	trust.noScan(t)
	status, _, raw = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Empty(t, rec.ShortSummary, "a refused write leaves the stored rating alone")

	trust.answer("hold", nil)
	status, _, raw = put(liveThirdPartyToken, "命中观察词的短评")
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "命中观察词的短评", trust.nextScan(t).Text)

	trust.answer("", errors.New("trust down"))
	status, rec, raw = put(liveThirdPartyToken, "检查服务挂了也能写")
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "检查服务挂了也能写", rec.ShortSummary, "the check fails open")
	require.Equal(t, "检查服务挂了也能写", trust.nextScan(t).Text)

	recordPath := "/v2/me/work-records/" + idstr(fx.Work)
	trust.answer("deny", nil)
	status, _, raw = liveDo(t, env, http.MethodPut, recordPath, liveThirdPartyToken,
		`{"work_state":{"state":"doing"},"rating":{"overall":6,"recommend":"no","short_summary":"又被拒绝"}}`)
	require.Equal(t, 422, status, string(raw))
	require.Contains(t, string(raw), `"pointer":"/rating/short_summary"`)
	trust.noScan(t)
	var wr liveWorkRecord
	status, _, raw = liveDo(t, env, http.MethodGet, recordPath, liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &wr))
	require.Nil(t, wr.WorkState, "a refused record writes no part")
	require.Equal(t, 8, wr.Rating.Overall)

	trust.answer("allow", nil)
	status, _, raw = liveDo(t, env, http.MethodPut, recordPath, liveThirdPartyToken,
		`{"work_state":{"state":"doing"},"rating":{"overall":6,"recommend":"no","short_summary":"经第三方表单写入"}}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &wr))
	require.Equal(t, 6, wr.Rating.Overall)
	scan = trust.nextScan(t)
	require.Equal(t, wr.Rating.ID, scan.SubjectID)
	require.Equal(t, "经第三方表单写入", scan.Text)
}
