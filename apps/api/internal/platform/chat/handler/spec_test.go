package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/chat/dto"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type compiledSpec struct {
	doc      *huma.OpenAPI
	compiler *jsonschema.Compiler
}

var (
	publishedOnce sync.Once
	published     *compiledSpec
)

func compileSpec(t *testing.T, doc *huma.OpenAPI) *compiledSpec {
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
	if err := c.AddResource("mem://chat.json", root); err != nil {
		t.Fatal(err)
	}
	return &compiledSpec{doc: doc, compiler: c}
}

func publishedSpec(t *testing.T) *compiledSpec {
	t.Helper()
	publishedOnce.Do(func() { published = compileSpec(t, Setup(fiber.New(), Options{}).OpenAPI()) })
	return published
}

func (s *compiledSpec) schema(t *testing.T, pointer string) *jsonschema.Schema {
	t.Helper()
	sch, err := s.compiler.Compile("mem://chat.json#" + pointer)
	if err != nil {
		t.Fatalf("compile %s: %v", pointer, err)
	}
	return sch
}

func (s *compiledSpec) operation(method, path string) (string, *huma.Operation) {
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
		op := map[string]*huma.Operation{http.MethodGet: item.Get, http.MethodPut: item.Put, http.MethodPost: item.Post,
			http.MethodPatch: item.Patch, http.MethodDelete: item.Delete}[method]
		if op != nil {
			return tmpl, op
		}
	}
	return "", nil
}

func pointerEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func conforms(t *testing.T, method, path string, status int, contentType string, raw []byte) {
	t.Helper()
	if len(raw) == 0 || !strings.Contains(contentType, "json") {
		return
	}
	spec := publishedSpec(t)
	pointer := "/components/schemas/Problem"
	if status < 400 {
		tmpl, op := spec.operation(method, path)
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
	if err := spec.schema(t, pointer).Validate(inst); err != nil {
		t.Errorf("%s %s: %d body does not match the document: %v\n%s", method, path, status, err, raw)
	}
}

func TestWireShapesAreWhatTheDocumentWouldHaveRejected(t *testing.T) {
	plain := dto.Message{Object: "message", ID: "1", ConversationID: "1", Seq: 1, SenderID: "1", Kind: "message",
		Text: "hi", Entities: []dto.Entity{}, Reactions: []dto.ReactionCount{}, CreatedAt: time.Unix(0, 0).UTC()}
	fail := problem.New(problem.CodeScopeRequired, "req_01M3J0BZ6QGTG9FT9DCR5K3KQQ", "/v2/chat/state", "")
	instance := func(v any) any {
		raw, _ := json.Marshal(v)
		inst, _ := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		return inst
	}

	skipWireShapes = true
	before := compileSpec(t, Setup(fiber.New(), Options{}).OpenAPI())
	skipWireShapes = false
	if before.schema(t, "/components/schemas/Message").Validate(instance(plain)) == nil {
		t.Fatal("control: a message without media must fail the unpatched document")
	}

	after := publishedSpec(t)
	if err := after.schema(t, "/components/schemas/Message").Validate(instance(plain)); err != nil {
		t.Fatalf("a message without media: %v", err)
	}
	if err := after.schema(t, "/components/schemas/Problem").Validate(instance(fail)); err != nil {
		t.Fatalf("a scope problem: %v", err)
	}
	withNull := instance(plain).(map[string]any)
	withNull["entities"] = nil
	if after.schema(t, "/components/schemas/Message").Validate(withNull) == nil {
		t.Fatal("entities is never null on the wire, so the document must not admit it")
	}
}
