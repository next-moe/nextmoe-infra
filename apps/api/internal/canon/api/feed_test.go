package api

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/commit"
)

func TestFeedSearchAndETag(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	h, _ := New(client)
	var start ChangeList
	for {
		var page ChangeList
		get(t, h, "/v3/catalog/changes?limit=500&cursor="+start.NextCursor, &page)
		if len(page.Items) == 0 {
			break
		}
		start = page
	}
	title := fmt.Sprintf("検索テスト%d", rand.Int64())
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "t"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": v("novel")}},
		{Entity: "work_title", Values: map[string]commit.Value{"work_id": v("ref:w"), "text": v(title), "lang": v("ja"), "kind": v("official")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	w := id(res.IDs["w"])

	var feed ChangeList
	get(t, h, "/v3/catalog/changes?cursor="+start.NextCursor, &feed)
	if len(feed.Items) != 2 || feed.Items[0].Entity != "work" || feed.Items[1].Entity != "work" || feed.Items[1].EntityID != w {
		t.Errorf("feed %+v", feed.Items)
	}

	var found WorkList
	get(t, h, "/v3/catalog/search?q="+url.QueryEscape(title[:len(title)-3]), &found)
	if len(found.Items) == 0 || found.Items[0].ID != w {
		t.Errorf("search %+v", found.Items)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v3/catalog/works/"+w, nil))
	tag := rec.Header().Get("ETag")
	req := httptest.NewRequest(http.MethodGet, "/v3/catalog/works/"+w, nil)
	req.Header.Set("If-None-Match", tag)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if tag == "" || rec2.Code != http.StatusNotModified || rec2.Body.Len() != 0 {
		t.Errorf("etag %q → %d (%d bytes)", tag, rec2.Code, rec2.Body.Len())
	}
}
