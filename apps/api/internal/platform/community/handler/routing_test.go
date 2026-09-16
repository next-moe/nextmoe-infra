package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/community/service"
	siteModel "api/internal/platform/site/model"

	"github.com/gofiber/fiber/v3"
)

// The other handler tests call the handler methods directly, so they cannot see
// a path that never reaches its handler — a literal segment shadowed by an
// {id} route, say. This one goes through the real router.
func TestRouting_EveryNewPathResolves(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	app := fiber.New()
	app.Use("/api/v1/community", func(c fiber.Ctx) error {
		c.Locals(localClient, &siteModel.OAuthClient{ID: "letmoe", CatalogSite: "letmoe"})
		return c.Next()
	})
	Setup(app, Services{
		Threads:    service.NewThreadService(testDB, sink),
		Posts:      service.NewPostService(testDB, sink),
		Trust:      service.NewTrustService(testDB),
		Engagement: service.NewEngagementService(testDB),
		Search:     service.NewSearchService(testDB),
		Boards:     service.NewBoardService(testDB),
	})

	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/api/v1/community/posts", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/comments?anchor_kind=1&anchor_id=g1", "", http.StatusOK},
		{http.MethodPost, "/api/v1/community/comments", `{"anchor_kind":1,"anchor_id":"g1","content_rating":0,"author_id":1,"body":"hi"}`, http.StatusOK},
		// The deprecated face keeps its own path; POST /comments must not swallow it.
		{http.MethodPost, "/api/v1/community/comments/resolve", `{"anchor_kind":1,"anchor_id":"g2","content_rating":0}`, http.StatusOK},
		{http.MethodGet, "/api/v1/community/comments?anchor_kind=0&anchor_id=b1", "", http.StatusUnprocessableEntity},
		{http.MethodGet, "/api/v1/community/threads?kind=0&sort=created", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/search/posts?q=%E6%B1%89%E5%8C%96", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/search/threads?q=%E6%B1%89%E5%8C%96", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/users/1/unread", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/users/1/anchor-subscriptions", "", http.StatusOK},
		{http.MethodPost, "/api/v1/community/threads/states", `{"user_id":1,"thread_ids":[]}`, http.StatusOK},
		{http.MethodPost, "/api/v1/community/anchors/notification", `{"user_id":1,"anchor_kind":3,"anchor_id":"w1","level":3}`, http.StatusOK},
		{http.MethodPost, "/api/v1/community/anchors/notification", `{"user_id":1,"anchor_kind":3,"anchor_id":"w1","level":2}`, http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/v1/community/anchors/states", `{"user_id":1,"anchors":[]}`, http.StatusOK},
		// The literal segment must win over /threads/{id}; a shadowed route would
		// try to parse "states" as an id and answer 422 instead.
		{http.MethodPost, "/api/v1/community/threads/999999/read", `{"user_id":1,"last_read_post_number":1}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/community/threads/999999/notification", `{"user_id":1,"level":3}`, http.StatusNotFound},
		// Query validation declared in the spec is enforced by the router layer.
		{http.MethodGet, "/api/v1/community/threads?kind=0&sort=hottest", "", http.StatusUnprocessableEntity},
		{http.MethodGet, "/api/v1/community/threads?kind=0&pinned=sticky", "", http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/v1/community/boards", `{"actor_id":1,"slug":"general","name":"General"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/community/boards", `{"actor_id":1,"slug":"Bad Slug","name":"x"}`, http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/v1/community/boards", `{"actor_id":1,"slug":"x","name":"x","topic_min_trust_level":4}`, http.StatusUnprocessableEntity},
		{http.MethodGet, "/api/v1/community/boards", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/boards/1", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/boards/by-slug/general", "", http.StatusOK},
		{http.MethodPost, "/api/v1/community/boards/reorder", `{"actor_id":1,"board_ids":[1]}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/community/boards/1", `{"actor_id":1,"status":1}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/community/boards/1", `{"actor_id":1,"status":7}`, http.StatusUnprocessableEntity},
		{http.MethodGet, "/api/v1/community/threads?kind=0&board_id=1&subboards=true&pinned=exclude", "", http.StatusOK},
		{http.MethodPost, "/api/v1/community/threads/999999/move", `{"actor_id":1,"board_id":1}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/community/threads/999999/pin", `{"actor_id":1,"scope":1}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/community/threads/999999/close", `{"actor_id":1,"closed":true}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/community/threads/999999/answer", `{"actor_id":1,"post_id":0}`, http.StatusNotFound},
		{http.MethodDelete, "/api/v1/community/boards/1?actor_id=1", "", http.StatusOK},
		{http.MethodGet, "/api/v1/community/boards/1", "", http.StatusNotFound},
	}
	for _, c := range cases {
		var body *strings.Reader
		if c.body == "" {
			body = strings.NewReader("")
		} else {
			body = strings.NewReader(c.body)
		}
		req := httptest.NewRequest(c.method, c.path, body)
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		if resp.StatusCode != c.want {
			t.Errorf("%s %s: want %d, got %d", c.method, c.path, c.want, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}
