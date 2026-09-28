package wireshapetest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"api/pkg/wireshape"

	"github.com/danielgtaylor/huma/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Spec struct {
	doc         *huma.OpenAPI
	compiler    *jsonschema.Compiler
	errorSchema string
}

// Compile holds every error body to the component named errorSchema.
func Compile(t testing.TB, doc *huma.OpenAPI, errorSchema string) *Spec {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	root, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err := c.AddResource("mem://spec.json", root); err != nil {
		t.Fatal(err)
	}
	return &Spec{doc: doc, compiler: c, errorSchema: errorSchema}
}

func (s *Spec) Schema(t testing.TB, pointer string) *jsonschema.Schema {
	t.Helper()
	sch, err := s.compiler.Compile("mem://spec.json#" + pointer)
	if err != nil {
		t.Fatalf("compile %s: %v", pointer, err)
	}
	return sch
}

func (s *Spec) operation(method, path string) (string, *huma.Operation) {
	got := strings.Split(strings.SplitN(path, "?", 2)[0], "/")
	for tmpl, item := range s.doc.Paths {
		want := strings.Split(tmpl, "/")
		if len(want) != len(got) {
			continue
		}
		match := true
		for i := range want {
			if want[i] != got[i] && !strings.HasPrefix(want[i], "{") {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		for _, op := range wireshape.Operations(item) {
			if op.Method == method {
				return tmpl, op
			}
		}
	}
	return "", nil
}

func pointerEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// Conforms fails t unless raw is what the document says method path answers
// with status.
func (s *Spec) Conforms(t testing.TB, method, path string, status int, contentType string, raw []byte) {
	t.Helper()
	if len(raw) == 0 || !strings.Contains(contentType, "json") {
		return
	}
	pointer := "/components/schemas/" + s.errorSchema
	if status < 400 {
		tmpl, op := s.operation(method, path)
		if op == nil {
			t.Errorf("%s %s: answered %d but the document has no such operation", method, path, status)
			return
		}
		if op.Responses[strconv.Itoa(status)] == nil {
			t.Errorf("%s %s: answered %d, which the document does not declare", method, path, status)
			return
		}
		pointer = "/paths/" + pointerEscape(tmpl) + "/" + strings.ToLower(method) + "/responses/" + strconv.Itoa(status) +
			"/content/" + pointerEscape("application/json") + "/schema"
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Errorf("%s %s: body is not JSON: %v", method, path, err)
		return
	}
	if err := s.Schema(t, pointer).Validate(inst); err != nil {
		t.Errorf("%s %s: %d body does not match the document: %v\n%s", method, path, status, err, raw)
	}
}

// Returned holds what a handler returned, called directly rather than over
// HTTP, to the document's 200 response for method path.
func (s *Spec) Returned(t testing.TB, method, path string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("%s %s: marshal: %v", method, path, err)
	}
	s.Conforms(t, method, path, http.StatusOK, "application/json", raw)
}
