package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	siteModel "api/internal/platform/site/model"
	"api/internal/platform/trust/service"
	"api/pkg/wireshape/wireshapetest"

	"github.com/gofiber/fiber/v3"
)

var (
	s2sOnce, adminOnce sync.Once
	s2sSpec, adminSpec *wireshapetest.Spec
)

func publishedSpec(t *testing.T) *wireshapetest.Spec {
	t.Helper()
	s2sOnce.Do(func() {
		s2sSpec = wireshapetest.Compile(t, Setup(fiber.New(), nil, nil, nil, nil, nil).OpenAPI(), "HouseError")
	})
	return s2sSpec
}

func publishedAdminSpec(t *testing.T) *wireshapetest.Spec {
	t.Helper()
	adminOnce.Do(func() {
		adminSpec = wireshapetest.Compile(t, SetupAdmin(fiber.New(), nil, nil, nil, nil, nil, nil).OpenAPI(), "HouseError")
	})
	return adminSpec
}

func TestListsMatchTheDocument(t *testing.T) {
	if testDB == nil {
		t.Skip("no test database")
	}
	truncateReviewTables(t)
	s2s := fiber.New()
	s2s.Use("/api/v1/trust", func(c fiber.Ctx) error {
		c.Locals(localClient, &siteModel.OAuthClient{ID: "letmoe", CatalogSite: "letmoe"})
		return c.Next()
	})
	Setup(s2s, service.NewReportService(testDB, nil), service.NewRegistryService(testDB),
		service.NewForwardService(testDB, nil), service.NewScanService(testDB, nil), service.NewTermService(testDB, nil))

	admin := fiber.New()
	admin.Use("/api/v1/admin/trust", func(c fiber.Ctx) error {
		c.Locals("user_id", uint(1))
		c.Locals("user_global_roles", []string{"admin"})
		return c.Next()
	})
	SetupAdmin(admin, service.NewReviewService(testDB), service.NewRegistryService(testDB), service.NewDispositionService(testDB),
		service.NewTermService(testDB, nil), service.NewPolicyService(testDB, service.PlatformDefaults{}), &fakeClients{})

	for _, c := range []struct {
		app              *fiber.App
		spec             *wireshapetest.Spec
		method, path, in string
	}{
		{s2s, publishedSpec(t), "GET", "/api/v1/trust/subject-kinds", ""},
		{s2s, publishedSpec(t), "GET", "/api/v1/trust/report-reasons", ""},
		{s2s, publishedSpec(t), "POST", "/api/v1/trust/check", `{"text":"nothing to see here"}`},
		{admin, publishedAdminSpec(t), "GET", "/api/v1/admin/trust/review-items", ""},
		{admin, publishedAdminSpec(t), "GET", "/api/v1/admin/trust/subject-kinds", ""},
		{admin, publishedAdminSpec(t), "GET", "/api/v1/admin/trust/report-reasons", ""},
		{admin, publishedAdminSpec(t), "GET", "/api/v1/admin/trust/dispositions", ""},
		{admin, publishedAdminSpec(t), "GET", "/api/v1/admin/trust/terms", ""},
		{admin, publishedAdminSpec(t), "GET", "/api/v1/admin/trust/site-policies", ""},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.in))
		if c.in != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s: %d %s", c.method, c.path, resp.StatusCode, raw)
			continue
		}
		c.spec.Conforms(t, c.method, c.path, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	}
}
