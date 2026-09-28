package handler

import (
	"io"
	"net/http"
	"sync"
	"testing"

	"api/internal/platform/community/service"
	siteModel "api/internal/platform/site/model"
	"api/pkg/wireshape/wireshapetest"

	"github.com/gofiber/fiber/v3"
)

var (
	publishedOnce sync.Once
	published     *wireshapetest.Spec
)

func publishedSpec(t *testing.T) *wireshapetest.Spec {
	t.Helper()
	publishedOnce.Do(func() { published = wireshapetest.Compile(t, Setup(fiber.New(), Services{}).OpenAPI(), "HouseError") })
	return published
}

func TestEveryListOnAnEmptySiteIsAnEmptyArray(t *testing.T) {
	cleanTables(t)
	app := fiber.New()
	app.Use("/api/v1/community", func(c fiber.Ctx) error {
		c.Locals(localClient, &siteModel.OAuthClient{ID: "letmoe", CatalogSite: "letmoe"})
		return c.Next()
	})
	sink := service.NoopSink{}
	Setup(app, Services{
		Threads: service.NewThreadService(testDB, sink), Posts: service.NewPostService(testDB, sink),
		Reactions: service.NewReactionService(testDB), Feedback: service.NewFeedbackService(testDB, sink),
		Flags: service.NewFlagService(testDB, sink), Trust: service.NewTrustService(testDB),
		Review: service.NewReviewService(testDB, sink), Engagement: service.NewEngagementService(testDB),
		Search: service.NewSearchService(testDB), Boards: service.NewBoardService(testDB),
		Notify: service.NewNotificationService(testDB), Follows: service.NewFollowService(testDB),
		Activities: service.NewActivityService(testDB),
	})
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/v1/community/activities", ""},
		{"GET", "/api/v1/community/anchor-presentations?anchor_kind=1&anchor_ids=1", ""},
		{"POST", "/api/v1/community/anchors/states", `{"user_id":1,"anchors":[{"anchor_kind":1,"anchor_id":"1"}]}`},
		{"GET", "/api/v1/community/authors/stats?ids=1", ""},
		{"GET", "/api/v1/community/authors/top", ""},
		{"GET", "/api/v1/community/authors/1/posts", ""},
		{"GET", "/api/v1/community/boards", ""},
		{"GET", "/api/v1/community/comments?anchor_kind=1&anchor_id=1", ""},
		{"GET", "/api/v1/community/notifications/feed", ""},
		{"GET", "/api/v1/community/posts", ""},
		{"POST", "/api/v1/community/posts/resolve", `{"ids":[1]}`},
		{"GET", "/api/v1/community/review", ""},
		{"GET", "/api/v1/community/search/posts?q=nothing", ""},
		{"GET", "/api/v1/community/search/threads?q=nothing", ""},
		{"GET", "/api/v1/community/threads", ""},
		{"POST", "/api/v1/community/threads/states", `{"user_id":1,"thread_ids":[1]}`},
		{"GET", "/api/v1/community/users/1/activities", ""},
		{"GET", "/api/v1/community/users/1/anchor-subscriptions", ""},
		{"GET", "/api/v1/community/users/1/blocking", ""},
		{"GET", "/api/v1/community/users/1/followers", ""},
		{"GET", "/api/v1/community/users/1/following", ""},
		{"GET", "/api/v1/community/users/1/following/activities", ""},
		{"GET", "/api/v1/community/users/1/notifications", ""},
		{"GET", "/api/v1/community/users/1/unread", ""},
	} {
		resp := doFollowReq(t, app, c.method, c.path, c.body)
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Errorf("%s %s: %d %s", c.method, c.path, resp.StatusCode, raw)
		}
	}
}
