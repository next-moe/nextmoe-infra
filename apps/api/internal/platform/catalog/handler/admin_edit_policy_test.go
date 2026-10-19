package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminGate_EditPolicyNeedsItsOwnPermission(t *testing.T) {
	cases := []struct {
		roles      []string
		wantStatus int
	}{
		{[]string{"moderator"}, fiber.StatusForbidden},
		{[]string{"creator"}, fiber.StatusForbidden},
		{[]string{"editor"}, fiber.StatusForbidden},
		{[]string{"admin"}, fiber.StatusOK},
		{[]string{"ren"}, fiber.StatusOK},
	}
	for _, tc := range cases {
		for _, path := range []string{AdminEditPolicyPrefix, AdminEditPolicyPrefix + "/overrides", AdminEditPolicyPrefix + "/audit"} {
			resp, err := adminGateApp(tc.roles, "").Test(httptest.NewRequest("GET", path, nil))
			require.NoError(t, err)
			assert.Equalf(t, tc.wantStatus, resp.StatusCode, "roles %v on %s", tc.roles, path)
		}
	}
	resp, err := adminGateApp([]string{"admin"}, "").Test(httptest.NewRequest("GET", "/api/v1/admin/catalog/edit-policyx", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode, "a sibling prefix is not the policy console")
}

func TestSetupAdmin_RegistersEditPolicyOperations(t *testing.T) {
	api := SetupAdmin(fiber.New(), nil, nil, nil, nil)
	RegisterEditPolicy(api, nil)
	paths := api.OpenAPI().Paths
	for _, p := range []string{AdminEditPolicyPrefix, AdminEditPolicyPrefix + "/overrides", AdminEditPolicyPrefix + "/audit"} {
		assert.NotNilf(t, paths[p], "operation %s must be registered", p)
	}
	app := fiber.New()
	RegisterEditPolicy(SetupAdmin(app, nil, nil, nil, nil), nil)
	status, _ := adminGet(t, app, AdminEditPolicyPrefix)
	assert.Equal(t, fiber.StatusServiceUnavailable, status, "an unbound store answers 503")
}

func adminSend(t *testing.T, app *fiber.App, method, url, payload string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(payload))
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestEditPolicyConsoleRoundTrip(t *testing.T) {
	db := openCatalogTestDB(t)
	for _, tbl := range []string{"edit_policy_override", "edit_policy_override_audit"} {
		require.NoError(t, db.Exec("TRUNCATE "+tbl+" RESTART IDENTITY").Error)
	}
	reg := editing.NewRegistry()
	require.NoError(t, editspec.RegisterAll(reg, db))
	store := editing.NewPolicyStore(db, reg)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", uint(42))
		return c.Next()
	})
	RegisterEditPolicy(SetupAdmin(app, nil, nil, nil, nil), store)

	status, body := adminGet(t, app, AdminEditPolicyPrefix)
	require.Equal(t, fiber.StatusOK, status, body)
	rows := body["data"].(map[string]any)["rows"].([]any)
	require.NotEmpty(t, rows)

	status, body = adminSend(t, app, "PUT", AdminEditPolicyPrefix+"/overrides",
		`{"entity_type":"catalog.work","field_key":"catalog.work.olang","propose":"locked"}`)
	require.Equal(t, fiber.StatusOK, status, body)
	spec, _ := reg.Type(editspec.TypeWork)
	assert.Equal(t, editing.ProposeLocked, spec.EffectivePolicy(editspec.FieldWorkOLang).Propose, "applies at once")

	status, body = adminSend(t, app, "PUT", AdminEditPolicyPrefix+"/overrides",
		`{"entity_type":"catalog.work","review":"perm:edit.catalog.nothing"}`)
	assert.Equal(t, fiber.StatusUnprocessableEntity, status, body)

	status, body = adminSend(t, app, "PUT", AdminEditPolicyPrefix+"/overrides",
		`{"entity_type":"catalog.ghost","propose":"open"}`)
	assert.Equal(t, fiber.StatusUnprocessableEntity, status, body)

	status, body = adminSend(t, app, "DELETE",
		AdminEditPolicyPrefix+"/overrides?entity_type=catalog.work&field_key=catalog.work.olang", "")
	require.Equal(t, fiber.StatusOK, status, body)
	assert.Equal(t, editing.ProposeOpen, spec.EffectivePolicy(editspec.FieldWorkOLang).Propose)

	status, _ = adminSend(t, app, "DELETE",
		AdminEditPolicyPrefix+"/overrides?entity_type=catalog.work&field_key=catalog.work.olang", "")
	assert.Equal(t, fiber.StatusNotFound, status)

	status, body = adminGet(t, app, AdminEditPolicyPrefix+"/audit")
	require.Equal(t, fiber.StatusOK, status, body)
	audit := body["data"].([]any)
	require.Len(t, audit, 2)
	latest := audit[0].(map[string]any)
	assert.Nil(t, latest["after"], "the newest entry is the removal")
	assert.EqualValues(t, 42, latest["actor_uid"])
}
