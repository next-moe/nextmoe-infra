package intromt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedChat struct {
	bodies []map[string]any
}

func (r *recordedChat) start(t *testing.T, replies ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(b, &body))
		r.bodies = append(r.bodies, body)
		n := len(r.bodies) - 1
		if n >= len(replies) {
			n = len(replies) - 1
		}
		_, _ = io.WriteString(w, replies[n])
	}))
}

func dropChatTemplateKwargs(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "chat_template_kwargs" {
			continue
		}
		out[k] = v
	}
	return out
}

func assertNoChatTemplateKwargs(t *testing.T, body map[string]any) {
	t.Helper()
	_, present := body["chat_template_kwargs"]
	assert.False(t, present)
}

func assertThinkingOff(t *testing.T, body map[string]any) {
	t.Helper()
	raw, ok := body["chat_template_kwargs"]
	require.True(t, ok)
	kw, ok := raw.(map[string]any)
	require.True(t, ok)
	v, ok := kw["enable_thinking"]
	require.True(t, ok)
	assert.Equal(t, false, v)
}

func TestAnAnswerFiledAsReasoningIsAskedAgainWithThinkingOff(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"推理里的译文"},"finish_reason":"stop"}]}`,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"  正文译文  "},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "正文译文", zh)
	require.Len(t, rec.bodies, 2)
	assertNoChatTemplateKwargs(t, rec.bodies[0])
	assertThinkingOff(t, rec.bodies[1])
	assert.Equal(t, dropChatTemplateKwargs(rec.bodies[0]), dropChatTemplateKwargs(rec.bodies[1]))
}

func TestAnEmptyReplyWithoutReasoningIsNotRetried(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		rec := &recordedChat{}
		srv := rec.start(t,
			`{"model":"m","choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`,
		)
		defer srv.Close()
		zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
		require.NoError(t, err)
		assert.Equal(t, "", zh)
		assert.Len(t, rec.bodies, 1)
	})
	t.Run("whitespace", func(t *testing.T) {
		rec := &recordedChat{}
		srv := rec.start(t,
			`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"   "},"finish_reason":"stop"}]}`,
		)
		defer srv.Close()
		zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
		require.NoError(t, err)
		assert.Equal(t, "", zh)
		assert.Len(t, rec.bodies, 1)
	})
}

func TestAThinkingOffRetryThatIsStillEmptyStops(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"x"},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "", zh)
	assert.Len(t, rec.bodies, 2)
}

func TestALengthStopGetsOneFreshRoll(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"半"},"finish_reason":"length"}]}`,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"译"},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "译", zh)
	require.Len(t, rec.bodies, 2)
	assert.Equal(t, rec.bodies[0], rec.bodies[1])
	assertNoChatTemplateKwargs(t, rec.bodies[0])
	assertNoChatTemplateKwargs(t, rec.bodies[1])
}

func TestTwoLengthStopsFail(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"半"},"finish_reason":"length"}]}`,
	)
	defer srv.Close()

	_, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, `finish_reason="length"`)
	assert.Len(t, rec.bodies, 2)
}

func TestALengthStopThenAnAnswerInReasoningMakesThreeCalls(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"半"},"finish_reason":"length"}]}`,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"r"},"finish_reason":"stop"}]}`,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"译"},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "译", zh)
	require.Len(t, rec.bodies, 3)
	assertNoChatTemplateKwargs(t, rec.bodies[0])
	assertNoChatTemplateKwargs(t, rec.bodies[1])
	assertThinkingOff(t, rec.bodies[2])
}

func TestAPlainAnswerMakesOneCall(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"译","reasoning_content":"thinking about the translation"},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "译", zh)
	assert.Len(t, rec.bodies, 1)
}

func TestTheRetryKeepsTheReasoningEffort(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"推理里的译文"},"finish_reason":"stop"}]}`,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"译"},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	tr := NewHTTPTranslator(srv.URL, "t", "m", 64)
	tr.SetEffort("low")
	_, _, err := tr.Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	require.Len(t, rec.bodies, 2)
	assert.Equal(t, "low", rec.bodies[0]["reasoning_effort"])
	assert.Equal(t, "low", rec.bodies[1]["reasoning_effort"])
	assertThinkingOff(t, rec.bodies[1])
}

func TestAThinkingOffRetryThatHitsTheCeilingFails(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"r"},"finish_reason":"stop"}]}`,
		`{"model":"m","choices":[{"message":{"role":"assistant","content":"半"},"finish_reason":"length"}]}`,
	)
	defer srv.Close()

	zh, _, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	assert.ErrorContains(t, err, `finish_reason="length"`)
	assert.Equal(t, "", zh)
	assert.Len(t, rec.bodies, 2)
}

func TestTheRetryReportsTheModelThatAnswered(t *testing.T) {
	rec := &recordedChat{}
	srv := rec.start(t,
		`{"model":"first","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"r"},"finish_reason":"stop"}]}`,
		`{"model":"second","choices":[{"message":{"role":"assistant","content":"译"},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	zh, model, err := NewHTTPTranslator(srv.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "译", zh)
	assert.Equal(t, "second", model)
}
