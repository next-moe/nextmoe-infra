package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestSpecExport(t *testing.T) {
	api := Setup(fiber.New(), nil, nil, nil, nil, nil)
	b, err := api.OpenAPI().YAML()
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	spec := string(b)
	for _, want := range []string{
		"/api/v1/trust/reports",
		"/api/v1/trust/subject-kinds",
		"/api/v1/trust/subject-kinds/ensure",
		"/api/v1/trust/forward",
		"/api/v1/trust/forward/resolve",
		"/api/v1/trust/scan",
		"/api/v1/trust/check",
		"operationId: submitReport",
		"operationId: forwardReviewItem",
		"operationId: submitScan",
		"operationId: checkText",
		"operationId: ensureSubjectKinds",
		"subject_kind",
		"reason_key",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("S2S spec missing %q", want)
		}
	}
}

func TestAdminSpecExport(t *testing.T) {
	api := SetupAdmin(fiber.New(), nil, nil, nil, nil, nil, nil)
	b, err := api.OpenAPI().YAML()
	if err != nil {
		t.Fatalf("marshal admin spec: %v", err)
	}
	spec := string(b)
	for _, want := range []string{
		"/api/v1/admin/trust/review-items",
		"/api/v1/admin/trust/review-items/{id}/claim",
		"/api/v1/admin/trust/review-items/{id}/decide",
		"/api/v1/admin/trust/subject-kinds",
		"/api/v1/admin/trust/subject-kinds/batch",
		"/api/v1/admin/trust/report-reasons",
		"/api/v1/admin/trust/dispositions",
		"/api/v1/admin/trust/terms",
		"/api/v1/admin/trust/terms/{id}/deprecate",
		"operationId: decideTrustReviewItem",
		"operationId: batchTrustSubjectKinds",
		"operationId: createTrustTerm",
		"operationId: deprecateTrustTerm",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("admin spec missing %q", want)
		}
	}
}

func TestAuthorIDMustBePositiveWhenSent(t *testing.T) {
	app := fiber.New()
	Setup(app, nil, nil, nil, nil, nil)
	post := func(path, body string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		return res.StatusCode
	}
	report := `{"subject_kind":"forum_topic","subject_id":"1","reason_key":"abuse","reporter_id":1%s}`
	forward := `{"site":"kungal","subject_kind":"community_post","subject_id":"1"%s}`
	for _, tc := range []struct {
		path, body string
		want       int
	}{
		{"/api/v1/trust/reports", fmt.Sprintf(report, `,"author_id":0`), http.StatusUnprocessableEntity},
		{"/api/v1/trust/forward", fmt.Sprintf(forward, `,"author_id":0`), http.StatusUnprocessableEntity},
		// Past validation, an unauthenticated call stops at the client binding.
		{"/api/v1/trust/reports", fmt.Sprintf(report, `,"author_id":7`), http.StatusForbidden},
		{"/api/v1/trust/reports", fmt.Sprintf(report, ``), http.StatusForbidden},
		{"/api/v1/trust/forward", fmt.Sprintf(forward, `,"author_id":7`), http.StatusForbidden},
	} {
		if got := post(tc.path, tc.body); got != tc.want {
			t.Errorf("POST %s %s = %d, want %d", tc.path, tc.body, got, tc.want)
		}
	}
}
