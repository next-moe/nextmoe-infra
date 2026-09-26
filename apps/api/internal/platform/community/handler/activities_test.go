package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/service"
	siteModel "api/internal/platform/site/model"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
)

func activityRouter(withSite bool) *fiber.App {
	app := fiber.New()
	if withSite {
		app.Use("/api/v1/community", func(c fiber.Ctx) error {
			c.Locals(localClient, &siteModel.OAuthClient{
				ID: "kungal", CatalogSite: "kungal",
				RedirectURIs: datatypes.JSON(`["http://127.0.0.1:2333/auth/callback","https://www.kungal.com/auth/callback"]`),
			})
			return c.Next()
		})
	}
	Setup(app, Services{
		Activities: service.NewActivityService(testDB),
		Follows:    service.NewFollowService(testDB),
		Notify:     service.NewNotificationService(testDB),
	})
	return app
}

func decodeData[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var env Envelope[T]
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Data
}

func activityJSON(key string, actor int64, at time.Time, extra string) string {
	return fmt.Sprintf(`{"key":%q,"actor_id":%d,"revision":%d,"verb":"publish","object_kind":"topic",
		"object_label":"话题","title":"t %s","url":"https://www.kungal.com/topic/%s","content_limit":"sfw",
		"occurred_at":%q%s}`, key, actor, time.Now().UnixMicro(), key, key, at.Format(time.RFC3339Nano), extra)
}

func TestActivityFacesThroughRouter(t *testing.T) {
	cleanTables(t)
	app := activityRouter(true)
	at := time.Now().Add(-time.Hour)

	body := `{"items":[` + activityJSON("topic:1", 7, at, `,"notify":true`) + `,` +
		activityJSON("topic:2", 7, at.Add(time.Minute), "") + `,` +
		strings.Replace(activityJSON("topic:3", 7, at, ""), "https://www.kungal.com", "http://127.0.0.1:2333", 1) + `,` +
		`{"key":"topic:9","actor_id":7,"revision":1,"removed":true}]}`
	resp := doFollowReq(t, app, http.MethodPost, "/api/v1/community/activities", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST activities: %d", resp.StatusCode)
	}
	out := decodeData[dto.ActivityWriteResponse](t, resp)
	got := []string{}
	for _, r := range out.Results {
		got = append(got, r.Key+"="+r.Outcome)
	}
	if strings.Join(got, ",") != "topic:1=created,topic:2=created,topic:3=invalid,topic:9=removed" {
		t.Fatalf("outcomes: %v (%+v)", got, out.Results)
	}

	listed := decodeData[dto.SiteActivityListResponse](t, doFollowReq(t, app, http.MethodGet, "/api/v1/community/activities?limit=2", ""))
	if len(listed.Activities) != 2 || listed.NextCursor == "" || listed.Activities[0].Key != "topic:1" ||
		listed.Activities[0].Verb != "publish" || listed.Activities[0].ContentLimit != "sfw" || listed.Activities[0].Title != "t topic:1" {
		t.Fatalf("reconciliation page 1: %+v", listed)
	}
	rest := decodeData[dto.SiteActivityListResponse](t, doFollowReq(t, app, http.MethodGet, "/api/v1/community/activities?limit=2&cursor="+listed.NextCursor, ""))
	if len(rest.Activities) != 1 || !rest.Activities[0].Removed || rest.NextCursor != "" {
		t.Fatalf("reconciliation lists tombstones: %+v", rest)
	}

	follow := doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/7", "")
	if fr := decodeData[dto.FollowResult](t, follow); fr.Notify != "all" || !fr.Created {
		t.Fatalf("a new follow echoes notify=all: %+v", fr)
	}
	feed := decodeData[dto.ActivityGroupListResponse](t, doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/1/following/activities?limit=1", ""))
	if len(feed.Groups) != 1 || feed.Groups[0].ItemCount != 2 || len(feed.Groups[0].Items) != 2 ||
		feed.Groups[0].Items[0].Key != "topic:2" || feed.Groups[0].Verb != "publish" || feed.NextCursor == "" {
		t.Fatalf("following feed: %+v", feed)
	}
	next := decodeData[dto.ActivityGroupListResponse](t, doFollowReq(t, app, http.MethodGet,
		"/api/v1/community/users/1/following/activities?limit=1&cursor="+feed.NextCursor, ""))
	if len(next.Groups) != 0 || next.NextCursor != "" {
		t.Fatalf("second page: %+v", next)
	}
	items := decodeData[dto.ActivityItemListResponse](t, doFollowReq(t, app, http.MethodGet,
		fmt.Sprintf("/api/v1/community/activity-groups/%d/items?content_limit=sfw", feed.Groups[0].ID), ""))
	if len(items.Items) != 2 {
		t.Fatalf("group items: %+v", items)
	}
	own := decodeData[dto.ActivityGroupListResponse](t, doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/7/activities?verbs=publish&sites=kungal", ""))
	if len(own.Groups) != 1 {
		t.Fatalf("user activities: %+v", own)
	}

	unseen := decodeData[dto.ActivityUnseenResponse](t, doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/1/following/activities/unseen", ""))
	if unseen.UnseenCount != 0 || unseen.SeenAt != nil {
		t.Fatalf("activity from before the follow is not unseen: %+v", unseen)
	}
	seen := decodeData[dto.ActivitySeenResponse](t, doFollowReq(t, app, http.MethodPost, "/api/v1/community/users/1/following/activities/seen", `{}`))
	if seen.SeenAt.IsZero() {
		t.Fatal("seen returns the mark")
	}

	for path, want := range map[string]int{
		"/api/v1/community/users/1/following/activities?cursor=%%%":        http.StatusBadRequest,
		"/api/v1/community/users/1/following/activities?verbs=shout":       http.StatusBadRequest,
		"/api/v1/community/users/1/following/activities?sites=Bad!":        http.StatusBadRequest,
		"/api/v1/community/users/1/following/activities?content_limit=r18": http.StatusUnprocessableEntity,
		"/api/v1/community/activity-groups/99999/items":                    http.StatusNotFound,
		"/api/v1/community/activities?cursor=x":                            http.StatusBadRequest,
	} {
		r := doFollowReq(t, app, http.MethodGet, path, "")
		if r.StatusCode != want {
			t.Errorf("GET %s: want %d, got %d", path, want, r.StatusCode)
		}
		_ = r.Body.Close()
	}
	tooMany := `{"items":[` + strings.Repeat(activityJSON("k", 7, at, "")+",", 100) + activityJSON("k", 7, at, "") + `]}`
	if r := doFollowReq(t, app, http.MethodPost, "/api/v1/community/activities", tooMany); r.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("101 items: want 422, got %d", r.StatusCode)
	}
}

func TestFollowNotifyFacesThroughRouter(t *testing.T) {
	cleanTables(t)
	app := activityRouter(true)

	missing := doFollowReq(t, app, http.MethodPatch, "/api/v1/community/users/1/following/2", `{"notify":"feed"}`)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("PATCH without a follow: want 404, got %d", missing.StatusCode)
	}
	_ = missing.Body.Close()
	_ = doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/2", "").Body.Close()

	patched := decodeData[dto.FollowNotifyResult](t, doFollowReq(t, app, http.MethodPatch, "/api/v1/community/users/1/following/2", `{"notify":"feed"}`))
	if patched.Notify != "feed" {
		t.Fatalf("PATCH: %+v", patched)
	}
	bad := doFollowReq(t, app, http.MethodPatch, "/api/v1/community/users/1/following/2", `{"notify":"loud"}`)
	if bad.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unknown notify: want 422, got %d", bad.StatusCode)
	}
	_ = bad.Body.Close()

	again := decodeData[dto.FollowResult](t, doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/2", ""))
	if again.Created || again.Notify != "feed" {
		t.Fatalf("PUT on an existing follow echoes its level: %+v", again)
	}
	states := decodeData[dto.FollowStatesResponse](t, doFollowReq(t, app, http.MethodPost, "/api/v1/community/follows/states", `{"viewer_id":1,"user_ids":[2,3]}`))
	if len(states.States) != 2 || states.States[0].ViewerNotify == nil || *states.States[0].ViewerNotify != "feed" ||
		states.States[1].ViewerNotify != nil {
		t.Fatalf("states viewer_notify: %+v", states)
	}

	followers := doFollowReq(t, app, http.MethodGet, "/api/v1/community/users/2/followers", "")
	raw := decodeData[json.RawMessage](t, followers)
	if strings.Contains(string(raw), "notify") {
		t.Fatalf("the public follower list must not reveal levels: %s", raw)
	}
}

func TestNotificationFeedCarriesTheActivity(t *testing.T) {
	cleanTables(t)
	app := activityRouter(true)
	_ = doFollowReq(t, app, http.MethodPut, "/api/v1/community/users/1/following/7", "").Body.Close()
	at := time.Now().Add(-time.Hour)
	_ = doFollowReq(t, app, http.MethodPost, "/api/v1/community/activities",
		`{"items":[`+activityJSON("topic:1", 7, at, `,"notify":true`)+`]}`).Body.Close()
	if _, _, _, err := service.NewNotificationService(testDB).ProcessBatch(context.Background()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	feed := decodeData[dto.NotificationFeedResponse](t, doFollowReq(t, app, http.MethodGet, "/api/v1/community/notifications/feed", ""))
	var kind10 *dto.NotificationView
	for i := range feed.Notifications {
		if feed.Notifications[i].Kind == 10 {
			kind10 = &feed.Notifications[i]
		}
	}
	if kind10 == nil || kind10.Activity == nil || kind10.Activity.Key != "topic:1" || kind10.Activity.Site != "kungal" ||
		kind10.Activity.URL != "https://www.kungal.com/topic/topic:1" || kind10.Activity.Verb != "publish" {
		t.Fatalf("kind 10 carries its activity: %+v", feed.Notifications)
	}
	for _, n := range feed.Notifications {
		if n.Kind != 10 && n.Activity != nil {
			t.Fatalf("only kind 10 carries an activity: %+v", n)
		}
	}
}

func TestActivityFacesRequireSiteBinding(t *testing.T) {
	cleanTables(t)
	app := activityRouter(false)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/community/activities", `{"items":[{"key":"k","actor_id":1,"revision":1,"removed":true}]}`},
		{http.MethodGet, "/api/v1/community/activities", ""},
		{http.MethodGet, "/api/v1/community/users/1/following/activities", ""},
		{http.MethodPatch, "/api/v1/community/users/1/following/2", `{"notify":"feed"}`},
	} {
		r := doFollowReq(t, app, c.method, c.path, c.body)
		if r.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s without a site: want 403, got %d", c.method, c.path, r.StatusCode)
		}
		_ = r.Body.Close()
	}
}
