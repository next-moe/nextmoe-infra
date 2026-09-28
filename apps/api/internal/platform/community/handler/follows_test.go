package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/service"
	siteModel "api/internal/platform/site/model"

	"github.com/gofiber/fiber/v3"
)

func followRouter(withSite bool) *fiber.App {
	app := fiber.New()
	if withSite {
		app.Use("/api/v1/community", func(c fiber.Ctx) error {
			c.Locals(localClient, &siteModel.OAuthClient{ID: "letmoe", CatalogSite: "letmoe"})
			return c.Next()
		})
	}
	Setup(app, Services{Follows: service.NewFollowService(testDB)})
	return app
}

func doFollowReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	var r io.Reader = http.NoBody
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	publishedSpec(t).Conforms(t, method, path, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp
}

func TestFollowFacesThroughRouter(t *testing.T) {
	cleanTables(t)
	app := followRouter(true)

	put := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/2", "")
	if put.StatusCode != http.StatusOK {
		t.Errorf("PUT following/2: want 200, got %d", put.StatusCode)
	}
	var putEnv Envelope[dto.FollowResult]
	if err := json.NewDecoder(put.Body).Decode(&putEnv); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	_ = put.Body.Close()
	if !putEnv.Data.Following || !putEnv.Data.Created || putEnv.Data.FollowerID != 1 || putEnv.Data.FolloweeID != 2 {
		t.Fatalf("PUT body: %+v", putEnv.Data)
	}

	getFollowing := doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/1/following", "")
	if getFollowing.StatusCode != http.StatusOK {
		t.Errorf("GET following: want 200, got %d", getFollowing.StatusCode)
	}
	var listEnv Envelope[dto.FollowListResponse]
	if err := json.NewDecoder(getFollowing.Body).Decode(&listEnv); err != nil {
		t.Fatalf("decode GET following: %v", err)
	}
	_ = getFollowing.Body.Close()
	if len(listEnv.Data.Users) != 1 || listEnv.Data.Users[0].UserID != 2 {
		t.Fatalf("GET following must not be shadowed by PUT, got %+v", listEnv.Data)
	}

	getFollowers := doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/2/followers", "")
	if getFollowers.StatusCode != http.StatusOK {
		t.Errorf("GET followers: want 200, got %d", getFollowers.StatusCode)
	}
	var followersEnv Envelope[dto.FollowListResponse]
	if err := json.NewDecoder(getFollowers.Body).Decode(&followersEnv); err != nil {
		t.Fatalf("decode GET followers: %v", err)
	}
	_ = getFollowers.Body.Close()
	if len(followersEnv.Data.Users) != 1 || followersEnv.Data.Users[0].UserID != 1 {
		t.Fatalf("GET users/2/followers must name the follower, got %+v", followersEnv.Data)
	}

	del := doFollowReq(t, app, http.MethodDelete, "/api/v1/community/users/1/following/2", "")
	if del.StatusCode != http.StatusOK {
		t.Errorf("DELETE following/2: want 200, got %d", del.StatusCode)
	}
	_ = del.Body.Close()

	states := doFollowReq(t, app, http.MethodPost, "/api/v1/community/follows/states", `{"viewer_id":1,"user_ids":[2]}`)
	if states.StatusCode != http.StatusOK {
		t.Errorf("POST states: want 200, got %d", states.StatusCode)
	}
	_ = states.Body.Close()

	self := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/1", "")
	if self.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("self follow: want 422, got %d", self.StatusCode)
	}
	_ = self.Body.Close()

	zero := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/0", "")
	if zero.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("target_id=0: want 422, got %d", zero.StatusCode)
	}
	_ = zero.Body.Close()

	ids := make([]string, 101)
	for i := range ids {
		ids[i] = strconv.Itoa(i + 1)
	}
	tooMany := doFollowReq(t, app, http.MethodPost, "/api/v1/community/follows/states",
		`{"viewer_id":1,"user_ids":[`+strings.Join(ids, ",")+`]}`)
	if tooMany.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("101 user_ids: want 422, got %d", tooMany.StatusCode)
	}
	_ = tooMany.Body.Close()

	badCursor := doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/1/followers?cursor=nope", "")
	if badCursor.StatusCode != http.StatusBadRequest {
		t.Errorf("bad cursor: want 400, got %d", badCursor.StatusCode)
	}
	_ = badCursor.Body.Close()
}

func TestFollowFacesRequireSiteBinding(t *testing.T) {
	cleanTables(t)
	app := followRouter(false)

	put := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/2", "")
	if put.StatusCode != http.StatusForbidden {
		t.Errorf("PUT without site: want 403, got %d", put.StatusCode)
	}
	_ = put.Body.Close()

	states := doFollowReq(t, app, http.MethodPost, "/api/v1/community/follows/states", `{"viewer_id":1,"user_ids":[2]}`)
	if states.StatusCode != http.StatusForbidden {
		t.Errorf("POST states without site: want 403, got %d", states.StatusCode)
	}
	_ = states.Body.Close()
}

func TestBlockFacesThroughRouter(t *testing.T) {
	cleanTables(t)
	app := followRouter(true)

	if r := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/2", ""); r.StatusCode != http.StatusOK {
		t.Fatalf("seed follow: %d", r.StatusCode)
	}
	put := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/2/blocking/1", "")
	if put.StatusCode != http.StatusOK {
		t.Fatalf("PUT blocking: want 200, got %d", put.StatusCode)
	}
	var putEnv Envelope[dto.BlockResult]
	if err := json.NewDecoder(put.Body).Decode(&putEnv); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	_ = put.Body.Close()
	if !putEnv.Data.Blocking || !putEnv.Data.Created || putEnv.Data.BlockerID != 2 || putEnv.Data.BlockedID != 1 {
		t.Fatalf("PUT body: %+v", putEnv.Data)
	}

	if r := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/2", ""); r.StatusCode != http.StatusForbidden {
		t.Fatalf("follow across a block: want 403, got %d", r.StatusCode)
	}

	states := doFollowReq(t, app, http.MethodPost, "/api/v1/community/follows/states", `{"viewer_id":1,"user_ids":[2]}`)
	var stEnv Envelope[dto.FollowStatesResponse]
	if err := json.NewDecoder(states.Body).Decode(&stEnv); err != nil {
		t.Fatalf("decode states: %v", err)
	}
	_ = states.Body.Close()
	if st := stEnv.Data.States[0]; !st.BlocksViewer || st.ViewerBlocks || st.ViewerFollows {
		t.Fatalf("states after block: %+v", st)
	}

	list := doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/2/blocking", "")
	var listEnv Envelope[dto.BlockListResponse]
	if err := json.NewDecoder(list.Body).Decode(&listEnv); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	_ = list.Body.Close()
	if len(listEnv.Data.Users) != 1 || listEnv.Data.Users[0].UserID != 1 || listEnv.Data.NextCursor != "" {
		t.Fatalf("list: %+v", listEnv.Data)
	}

	del := doFollowReq(t, app, http.MethodDelete, "/api/v1/community/users/2/blocking/1", "")
	var delEnv Envelope[dto.UnblockResult]
	if err := json.NewDecoder(del.Body).Decode(&delEnv); err != nil {
		t.Fatalf("decode DELETE: %v", err)
	}
	_ = del.Body.Close()
	if delEnv.Data.Blocking || !delEnv.Data.Deleted {
		t.Fatalf("DELETE body: %+v", delEnv.Data)
	}

	noSite := followRouter(false)
	if r := doFollowReq(t, noSite, http.MethodPut, "/api/v1/community/users/2/blocking/1", ""); r.StatusCode == http.StatusOK {
		t.Fatal("a client with no site binding must not block")
	}
}
