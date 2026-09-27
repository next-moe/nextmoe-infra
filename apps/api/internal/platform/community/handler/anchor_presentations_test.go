package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"api/internal/platform/community/dto"
)

func TestAnchorPresentationFacesThroughRouter(t *testing.T) {
	cleanTables(t)
	app := activityRouter(true)
	rev := time.Now().UnixMicro()
	body := fmt.Sprintf(`{"items":[
		{"anchor_kind":1,"anchor_id":"123","revision":%d,"title":"千恋＊万花","url":"https://www.kungal.com/galgame/123","work_id":9001,"content_limit":"nsfw"},
		{"anchor_kind":2,"anchor_id":"rating:45","revision":%d,"title":"评分","url":"http://127.0.0.1:2333/galgame-rating/45"},
		{"anchor_kind":2,"anchor_id":"website:94","revision":%d,"removed":true}]}`, rev, rev, rev)
	out := decodeData[dto.AnchorPresentationWriteResponse](t, doFollowReq(t, app, http.MethodPut, "/api/v1/community/anchor-presentations", body))
	if len(out.Results) != 3 || out.Results[0].Outcome != "created" || out.Results[1].Outcome != "invalid" ||
		out.Results[2].Outcome != "removed" || out.Results[1].AnchorID != "rating:45" {
		t.Fatalf("outcomes: %+v", out.Results)
	}

	page := decodeData[dto.AnchorPresentationListResponse](t, doFollowReq(t, app, http.MethodGet, "/api/v1/community/anchor-presentations?limit=1", ""))
	if len(page.Presentations) != 1 || page.NextCursor == "" || page.Presentations[0].AnchorID != "123" ||
		page.Presentations[0].ContentLimit != "nsfw" || page.Presentations[0].WorkID == nil || *page.Presentations[0].WorkID != 9001 {
		t.Fatalf("first page: %+v", page)
	}
	rest := decodeData[dto.AnchorPresentationListResponse](t, doFollowReq(t, app, http.MethodGet,
		"/api/v1/community/anchor-presentations?cursor="+page.NextCursor, ""))
	if len(rest.Presentations) != 1 || !rest.Presentations[0].Removed || rest.NextCursor != "" {
		t.Fatalf("tombstones are listed: %+v", rest)
	}

	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPut, "/api/v1/community/anchor-presentations", `{"items":[{"anchor_kind":0,"anchor_id":"1","revision":1,"removed":true}]}`, http.StatusUnprocessableEntity},
		{http.MethodPut, "/api/v1/community/anchor-presentations", `{"items":[]}`, http.StatusUnprocessableEntity},
		{http.MethodGet, "/api/v1/community/anchor-presentations?cursor=%%%", "", http.StatusBadRequest},
	} {
		r := doFollowReq(t, app, c.method, c.path, c.body)
		if r.StatusCode != c.want {
			t.Errorf("%s %s: want %d, got %d", c.method, c.path, c.want, r.StatusCode)
		}
		_ = r.Body.Close()
	}

	bare := activityRouter(false)
	r := doFollowReq(t, bare, http.MethodGet, "/api/v1/community/anchor-presentations", "")
	if r.StatusCode != http.StatusForbidden {
		t.Errorf("without a site: want 403, got %d", r.StatusCode)
	}
	_ = r.Body.Close()
}
