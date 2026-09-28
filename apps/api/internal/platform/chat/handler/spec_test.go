package handler

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/chat/dto"
	"api/pkg/wireshape"
	"api/pkg/wireshape/wireshapetest"

	"github.com/gofiber/fiber/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	publishedOnce sync.Once
	published     *wireshapetest.Spec
)

func publishedSpec(t *testing.T) *wireshapetest.Spec {
	t.Helper()
	publishedOnce.Do(func() { published = wireshapetest.Compile(t, Setup(fiber.New(), Options{}).OpenAPI(), "Problem") })
	return published
}

func conforms(t *testing.T, method, path string, status int, contentType string, raw []byte) {
	t.Helper()
	publishedSpec(t).Conforms(t, method, path, status, contentType, raw)
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

	wireshape.Skip = true
	before := wireshapetest.Compile(t, Setup(fiber.New(), Options{}).OpenAPI(), "Problem")
	wireshape.Skip = false
	if before.Schema(t, "/components/schemas/Message").Validate(instance(plain)) == nil {
		t.Fatal("control: a message without media must fail the unpatched document")
	}

	after := publishedSpec(t)
	if err := after.Schema(t, "/components/schemas/Message").Validate(instance(plain)); err != nil {
		t.Fatalf("a message without media: %v", err)
	}
	if err := after.Schema(t, "/components/schemas/Problem").Validate(instance(fail)); err != nil {
		t.Fatalf("a scope problem: %v", err)
	}
	withNull := instance(plain).(map[string]any)
	withNull["entities"] = nil
	if after.Schema(t, "/components/schemas/Message").Validate(withNull) == nil {
		t.Fatal("entities is never null on the wire, so the document must not admit it")
	}
}
