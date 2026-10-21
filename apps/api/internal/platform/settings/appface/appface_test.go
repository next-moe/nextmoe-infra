package appface_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v2handler "api/internal/platform/apiv2/handler"
	"api/internal/platform/settings"
	"api/internal/platform/settings/appface"
	"api/internal/platform/settings/keys"
	"api/pkg/wireshape/wireshapetest"

	"github.com/gofiber/fiber/v3"
)

var kungalSite = uint(2)

var clients = map[string]appface.Client{
	"kungal-app":         {SiteID: &kungalSite, Public: true},
	"kungal-app-desktop": {SiteID: &kungalSite, Public: true},
	"unbound-app":        {Public: true},
	"forum-backend":      {SiteID: &kungalSite, Public: false},
}

func lookup(_ context.Context, clientID string) (appface.Client, bool, error) {
	if clientID == "broken" {
		return appface.Client{}, false, errors.New("db down")
	}
	cl, ok := clients[clientID]
	return cl, ok, nil
}

type harness struct {
	app  *fiber.App
	spec *wireshapetest.Spec
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	app := fiber.New()
	v2handler.SetupWith(app, v2handler.Options{})
	api := appface.Setup(app, lookup)
	return &harness{app: app, spec: wireshapetest.Compile(t, api.OpenAPI(), "Problem")}
}

type reply struct {
	status int
	etag   string
	body   []byte
}

func (h *harness) get(t *testing.T, path, ifNoneMatch string) reply {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := h.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	h.spec.Conforms(t, http.MethodGet, path, resp.StatusCode, resp.Header.Get("Content-Type"), body)
	return reply{status: resp.StatusCode, etag: resp.Header.Get("ETag"), body: body}
}

func decode(t *testing.T, r reply) (siteID *float64, values map[string]any) {
	t.Helper()
	var out struct {
		ClientID string         `json:"client_id"`
		SiteID   *float64       `json:"site_id"`
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	return out.SiteID, out.Settings
}

func problemCode(t *testing.T, r reply) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatalf("decode problem %s: %v", r.body, err)
	}
	return p.Code
}

func TestAppReadsItsSitesPublicKeysAndItsOwnKeys(t *testing.T) {
	h := newHarness(t)
	settings.Override(t, keys.PlatformNotice, "platform-wide notice")
	settings.OverrideSite(t, keys.PlatformNotice, kungalSite, "kungal notice")
	settings.OverrideSite(t, keys.PlatformReadOnly, kungalSite+1, true)
	settings.Override(t, keys.AppKungalHomeTopicCount, 30)

	r := h.get(t, "/v2/apps/kungal-app/settings", "")
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
	siteID, got := decode(t, r)
	if siteID == nil || *siteID != 2 {
		t.Errorf("site_id = %v, want 2", siteID)
	}
	want := map[string]any{
		"platform.notice":              "kungal notice",
		"platform.read_only":           false,
		"image.upload_enabled":         false,
		"artifact.upload_enabled":      false,
		"app.kungal.home_topic_count":  float64(30),
		"app.kungal.home_work_rows":    float64(2),
		"app.kungal.home_rating_count": float64(6),
		"auth.name_change_cost":        float64(keys.AuthNameChangeCost.Get()),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v, want %#v", k, got[k], v)
		}
	}
	for k := range got {
		e, ok := keys.Live().Lookup(k)
		if !ok {
			t.Errorf("unknown key %q in the body", k)
			continue
		}
		if !e.Meta().Public && !strings.HasPrefix(k, "app.kungal.") {
			t.Errorf("%s is neither public nor this app's own key", k)
		}
	}
	if r.etag == "" || strings.HasPrefix(r.etag, "W/") {
		t.Errorf("ETag %q, want a strong validator", r.etag)
	}

	desktop := h.get(t, "/v2/apps/kungal-app-desktop/settings", "")
	if _, d := decode(t, desktop); d["app.kungal.home_topic_count"] != float64(30) {
		t.Errorf("the desktop client reads home_topic_count %v, want the same 30", d["app.kungal.home_topic_count"])
	}
}

func TestAnUnchangedBodyAnswers304AndAChangeDoesNot(t *testing.T) {
	h := newHarness(t)
	first := h.get(t, "/v2/apps/kungal-app/settings", "")
	if first.status != http.StatusOK {
		t.Fatalf("status %d: %s", first.status, first.body)
	}

	same := h.get(t, "/v2/apps/kungal-app/settings", first.etag)
	if same.status != http.StatusNotModified || len(same.body) != 0 {
		t.Fatalf("unchanged: status %d body %q, want 304 and no body", same.status, same.body)
	}

	settings.Override(t, keys.AppKungalHomeWorkRows, 3)
	changed := h.get(t, "/v2/apps/kungal-app/settings", first.etag)
	if changed.status != http.StatusOK {
		t.Fatalf("changed: status %d, want 200", changed.status)
	}
	if changed.etag == first.etag {
		t.Errorf("ETag did not move after a value changed")
	}
}

func TestAClientWithNoSiteOrNoAppKeys(t *testing.T) {
	h := newHarness(t)
	settings.Override(t, keys.PlatformNotice, "platform-wide notice")
	settings.OverrideSite(t, keys.PlatformNotice, kungalSite, "kungal notice")

	r := h.get(t, "/v2/apps/unbound-app/settings", "")
	if r.status != http.StatusOK {
		t.Fatalf("status %d: %s", r.status, r.body)
	}
	siteID, got := decode(t, r)
	if siteID != nil {
		t.Errorf("site_id = %v, want null", *siteID)
	}
	if got["platform.notice"] != "platform-wide notice" {
		t.Errorf("platform.notice = %v, want the platform value", got["platform.notice"])
	}
	for k := range got {
		if strings.HasPrefix(k, "app.") {
			t.Errorf("%s leaked to a client that is not the kungal app", k)
		}
	}
}

func TestRefusals(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{"/v2/apps/nobody/settings", http.StatusNotFound, "NOT_FOUND"},
		{"/v2/apps/forum-backend/settings", http.StatusNotFound, "NOT_FOUND"},
		{"/v2/apps/broken/settings", http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE"},
		{"/v2/apps/" + strings.Repeat("a", 51) + "/settings", http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"/v2/apps/bad%20id/settings", http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
	} {
		r := h.get(t, tc.path, "")
		if r.status != tc.status {
			t.Errorf("%s: status %d, want %d: %s", tc.path, r.status, tc.status, r.body)
			continue
		}
		if got := problemCode(t, r); got != tc.code {
			t.Errorf("%s: code %s, want %s", tc.path, got, tc.code)
		}
	}
}
