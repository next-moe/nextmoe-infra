package handler

import (
	"encoding/json"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func testSchemaRegistry(t *testing.T) *editing.Registry {
	t.Helper()
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, nil); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestGetSchemaUnbound(t *testing.T) {
	_, err := (*Catalog)(nil).GetSchema(t.Context(), "work")
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("%v", err)
	}
	_, err = (&Catalog{}).GetSchema(t.Context(), "trait")
	p, ok = err.(*problem.Problem)
	if !ok || p.Code != problem.CodeNotFound {
		t.Fatalf("trait %v", err)
	}
}

func TestGetSchemaWorkAndRelease(t *testing.T) {
	c := &Catalog{EditTypes: testSchemaRegistry(t)}
	s, err := c.GetSchema(t.Context(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if s.Object != "object_schema" || s.TargetObject != "work" || s.EntityType != editspec.TypeWork {
		t.Fatalf("%+v", s)
	}
	if s.CreationDisabled {
		t.Fatal("work creation is not disabled")
	}
	if len(s.Include) == 0 || len(s.FullSet) == 0 {
		t.Fatalf("include/full_set %+v %+v", s.Include, s.FullSet)
	}
	foundTitles, foundKind := false, false
	capByKey := map[string]int{}
	for _, f := range s.Fields {
		capByKey[f.Key] = f.MaxSuppressed
		if f.Key == editspec.FieldWorkTitles {
			foundTitles = true
			if f.FieldType != string(editing.KindList) {
				t.Fatalf("titles field_type %s", f.FieldType)
			}
			if f.MaxElements != editing.DefaultMaxElements && f.MaxElements <= 0 {
				t.Fatalf("titles max_elements %d", f.MaxElements)
			}
		}
		if f.Key == "kind" || f.FieldType == "kind" {
			foundKind = true
		}
	}
	if !foundTitles {
		t.Fatal("missing catalog.work.titles")
	}
	// titles declares no cap of its own, so a raw read published 0 here while
	// the companion enforced the 200 default — max_suppressed must always be
	// the effective limit, equal on parent and companion.
	suppressed := 0
	for key, parentCap := range capByKey {
		companionCap, ok := capByKey[key+editing.SuppressedFieldSuffix]
		if !ok {
			continue
		}
		suppressed++
		if parentCap != companionCap || parentCap <= 0 {
			t.Fatalf("%s max_suppressed %d != companion %d", key, parentCap, companionCap)
		}
	}
	if suppressed < 3 {
		t.Fatalf("expected titles, credits and roster companions, saw %d", suppressed)
	}
	if foundKind {
		t.Fatal("kind must not appear as a field key or field_type")
	}

	rel, err := c.GetSchema(t.Context(), "release")
	if err != nil {
		t.Fatal(err)
	}
	if !rel.CreationDisabled || rel.EntityType != editspec.TypeRelease {
		t.Fatalf("%+v", rel)
	}

	co, err := c.GetSchema(t.Context(), "company")
	if err != nil {
		t.Fatal(err)
	}
	if co.EntityType != editspec.TypeLabel || co.TargetObject != "company" {
		t.Fatalf("%+v", co)
	}
}

func TestCatalogSchemaHTTP(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	SetupWith(app, Options{Catalog: &Catalog{EditTypes: testSchemaRegistry(t)}})

	status, ct, body := do(t, app, httpMethodGet, "/v2/catalog/schemas/work")
	require.Equal(t, 200, status)
	require.Contains(t, ct, "json")
	var s repr.ObjectSchema
	require.NoError(t, json.Unmarshal(body, &s), string(body))
	require.Equal(t, "object_schema", s.Object)
	require.Equal(t, "work", s.TargetObject)
	require.Contains(t, s.Include, "titles")
	require.Contains(t, s.FullSet, "titles")
	require.NotContains(t, s.FullSet, "characters")
	require.False(t, s.CreationDisabled)
	require.NotEmpty(t, s.Fields)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	_, hasKind := raw["kind"]
	require.False(t, hasKind)
	_, hasPropose := raw["can_propose"]
	require.False(t, hasPropose)
	for _, item := range raw["fields"].([]any) {
		f := item.(map[string]any)
		if _, ok := f["kind"]; ok {
			t.Fatalf("field has kind: %v", f)
		}
		if _, ok := f["can_propose"]; ok {
			t.Fatalf("field has can_propose: %v", f)
		}
		if _, ok := f["field_type"]; !ok {
			t.Fatalf("field missing field_type: %v", f)
		}
	}

	status, _, body = do(t, app, httpMethodGet, "/v2/catalog/schemas/release")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &s))
	require.True(t, s.CreationDisabled)

	for _, object := range []string{"person", "credit_name"} {
		status, _, body = do(t, app, httpMethodGet, "/v2/catalog/schemas/"+object)
		require.Equal(t, 200, status, object)
		require.NoError(t, json.Unmarshal(body, &s))
		require.Equal(t, object, s.TargetObject)
		require.NotEmpty(t, s.Fields, object)
	}

	status, _, body = do(t, app, httpMethodGet, "/v2/catalog/schemas/trait")
	require.Equal(t, 404, status)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeNotFound, p.Code)
}

func TestSchemaFieldEncoding(t *testing.T) {
	c := &Catalog{EditTypes: testSchemaRegistry(t)}
	s, err := c.GetSchema(t.Context(), "work")
	require.NoError(t, err)
	enc := map[string]*string{}
	for _, f := range s.Fields {
		enc[f.Key] = f.Encoding
	}
	require.Contains(t, enc, editspec.FieldWorkContentRating)
	require.Contains(t, enc, editspec.FieldWorkOLang)
	require.Contains(t, enc, editspec.FieldWorkDisplayNSFW)
	require.Equal(t, "int", *enc[editspec.FieldWorkContentRating])
	require.Equal(t, "token", *enc[editspec.FieldWorkOLang])
	require.Nil(t, enc[editspec.FieldWorkDisplayNSFW])

	rel, err := c.GetSchema(t.Context(), "release")
	require.NoError(t, err)
	relEnc := map[string]*string{}
	for _, f := range rel.Fields {
		relEnc[f.Key] = f.Encoding
	}
	require.Equal(t, "int", *relEnc[editspec.FieldReleaseKind])
	require.Equal(t, "token", *relEnc[editspec.FieldReleasePlatform])

	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	SetupWith(app, Options{Catalog: c})
	status, _, body := do(t, app, httpMethodGet, "/v2/catalog/schemas/work")
	require.Equal(t, 200, status)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	for _, item := range raw["fields"].([]any) {
		f := item.(map[string]any)
		v, present := f["encoding"]
		if f["vocabulary"] == "" {
			require.False(t, present, "%v carries no vocabulary, so encoding must be absent", f["key"])
			continue
		}
		require.True(t, present, "%v names a vocabulary but publishes no encoding", f["key"])
		require.Contains(t, []any{"token", "int"}, v)
	}
}

const httpMethodGet = "GET"

func TestCatalogSchemaUnauthenticated(t *testing.T) {
	app := testApp(t)
	status, ct, body := do(t, app, httpMethodGet, "/v2/catalog/schemas/work")
	require.Equal(t, 503, status)
	require.Contains(t, ct, "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
	require.NotEqual(t, problem.CodeMissingCredential, p.Code)

	status, _, body = do(t, app, httpMethodGet, "/v2/catalog/schemas/nope")
	require.Equal(t, 404, status)
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeNotFound, p.Code)
}
