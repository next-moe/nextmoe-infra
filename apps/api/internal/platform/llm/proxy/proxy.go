package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"api/internal/platform/llm"

	"github.com/gofiber/fiber/v3"
)

type Server struct {
	GW *llm.Gateway
}

func Mount(app *fiber.App, s *Server) {
	v1 := app.Group("/v1", s.auth)
	v1.Get("/models", s.models)
	v1.Post("/chat/completions", s.chat)
	v1.Post("/embeddings", s.embeddings)
	v1.Post("/moderations", s.moderations)
	v1.Post("/images/generations", s.images)
}

const localKey = "llm_access_key"

func (s *Server) auth(c fiber.Ctx) error {
	secret, ok := strings.CutPrefix(c.Get("Authorization"), "Bearer ")
	k, found := s.GW.Authorize(strings.TrimSpace(secret))
	if !ok || !found {
		return fail(c, http.StatusUnauthorized, "invalid_api_key", "a valid NextMoe gateway access key is required")
	}
	s.GW.MarkUsed(k)
	c.Locals(localKey, k)
	return c.Next()
}

func keyOf(c fiber.Ctx) *llm.AccessKey {
	k, _ := c.Locals(localKey).(*llm.AccessKey)
	return k
}

func fail(c fiber.Ctx, status int, code, msg string) error {
	typ := "invalid_request_error"
	if status >= 500 {
		typ = "api_error"
	}
	return c.Status(status).JSON(fiber.Map{"error": fiber.Map{"message": msg, "type": typ, "code": code}})
}

func failErr(c fiber.Ctx, err error) error {
	var le *llm.Error
	switch {
	case errors.Is(err, llm.ErrUnknownModel), errors.Is(err, llm.ErrUnknownScene):
		return fail(c, http.StatusNotFound, "model_not_found", err.Error())
	case errors.As(err, &le) && le.Status > 0:
		return fail(c, le.Status, string(le.Kind), err.Error())
	case errors.As(err, &le):
		return fail(c, statusFor(le.Kind), string(le.Kind), err.Error())
	default:
		return fail(c, http.StatusBadGateway, "upstream", err.Error())
	}
}

func statusFor(k llm.ErrorKind) int {
	switch k {
	case llm.ErrKindNoRoute:
		return http.StatusServiceUnavailable
	case llm.ErrKindBadRequest, llm.ErrKindContextLength, llm.ErrKindContentFiltered:
		return http.StatusBadRequest
	case llm.ErrKindRateLimit:
		return http.StatusTooManyRequests
	case llm.ErrKindTimeout:
		return http.StatusGatewayTimeout
	case llm.ErrKindCanceled:
		return 499
	}
	return http.StatusBadGateway
}

func (sv *Server) decode(c fiber.Ctx) (map[string]json.RawMessage, string, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &body); err != nil {
		return nil, "", fail(c, http.StatusBadRequest, "invalid_json", "the request body must be a JSON object")
	}
	var model string
	if err := json.Unmarshal(body["model"], &model); err != nil || model == "" {
		return nil, "", fail(c, http.StatusBadRequest, "model_required", "model is required")
	}
	if !sv.GW.Allows(keyOf(c), model) {
		return nil, "", fail(c, http.StatusForbidden, "model_not_allowed", "this access key may not use "+model)
	}
	return body, model, nil
}

func site(c fiber.Ctx) string {
	return "key:" + keyOf(c).Name
}

func (sv *Server) models(c fiber.Ctx) error {
	k := keyOf(c)
	data := []fiber.Map{}
	for _, m := range sv.GW.Models() {
		if sv.GW.Allows(k, m.Key) {
			data = append(data, fiber.Map{"id": m.Key, "object": "model", "owned_by": "nextmoe", "kind": m.Kind})
		}
	}
	return c.JSON(fiber.Map{"object": "list", "data": data})
}

func (sv *Server) unary(c fiber.Ctx, call func(context.Context, llm.Request) (llm.Response, error)) error {
	body, model, err := sv.decode(c)
	if body == nil {
		return err
	}
	delete(body, "model")
	res, err := call(c.Context(), llm.Request{Model: model, Body: body, Site: site(c)})
	if err != nil {
		return failErr(c, err)
	}
	c.Set("Content-Type", "application/json")
	return c.Send(res.Body)
}

func (sv *Server) embeddings(c fiber.Ctx) error { return sv.unary(c, sv.GW.Embed) }

func (sv *Server) moderations(c fiber.Ctx) error { return sv.unary(c, sv.GW.Moderate) }

func (sv *Server) images(c fiber.Ctx) error { return sv.unary(c, sv.GW.GenerateImages) }

var chatKeys = map[string]bool{"model": true, "messages": true, "stream": true, "stream_options": true, "tools": true,
	"tool_choice": true, "max_tokens": true, "max_completion_tokens": true, "user": true}

type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func chatCall(body map[string]json.RawMessage, model, site string) (llm.Call, bool, bool, error) {
	var msgs []wireMessage
	if err := json.Unmarshal(body["messages"], &msgs); err != nil || len(msgs) == 0 {
		return llm.Call{}, false, false, errors.New("messages must be a non-empty array")
	}
	call := llm.Call{Model: model, Site: site, Extra: map[string]json.RawMessage{}}
	for _, m := range msgs {
		msg := llm.Message{Role: llm.Role(m.Role), ToolCallID: m.ToolCallID}
		var text string
		switch {
		case json.Unmarshal(m.Content, &text) == nil:
			msg.Content = text
		case len(m.Content) > 0 && string(m.Content) != "null":
			msg.Raw = m.Content
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		call.Messages = append(call.Messages, msg)
	}
	if raw, ok := body["tools"]; ok {
		var tools []struct {
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &tools); err != nil {
			return llm.Call{}, false, false, errors.New("tools must be an array of functions")
		}
		for _, t := range tools {
			call.Tools = append(call.Tools, llm.Tool{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters})
		}
	}
	if raw, ok := body["tool_choice"]; ok {
		if json.Unmarshal(raw, &call.ToolChoice) != nil {
			call.Extra["tool_choice"] = raw
		}
	}
	for _, k := range []string{"max_completion_tokens", "max_tokens"} {
		if raw, ok := body[k]; ok {
			_ = json.Unmarshal(raw, &call.MaxTokens)
		}
	}
	_ = json.Unmarshal(body["user"], &call.AffinityKey)
	for k, v := range body {
		if !chatKeys[k] {
			call.Extra[k] = v
		}
	}
	var stream bool
	_ = json.Unmarshal(body["stream"], &stream)
	var opts struct {
		IncludeUsage bool `json:"include_usage"`
	}
	_ = json.Unmarshal(body["stream_options"], &opts)
	return call, stream, opts.IncludeUsage, nil
}

func (sv *Server) chat(c fiber.Ctx) error {
	body, model, err := sv.decode(c)
	if body == nil {
		return err
	}
	call, stream, includeUsage, err := chatCall(body, model, site(c))
	if err != nil {
		return fail(c, http.StatusBadRequest, "invalid_messages", err.Error())
	}
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	if !stream {
		call.Buffered = true
		res, err := sv.GW.Stream(c.Context(), call, nil)
		if err != nil {
			return failErr(c, err)
		}
		return c.JSON(completion(id, created, model, res))
	}
	return sv.streamChat(c, call, id, created, model, includeUsage)
}

func completion(id string, created int64, model string, res llm.Result) fiber.Map {
	msg := fiber.Map{"role": "assistant", "content": res.Text}
	if res.Reasoning != "" {
		msg["reasoning_content"] = res.Reasoning
	}
	if len(res.ToolCalls) > 0 {
		msg["tool_calls"] = wireToolCalls(res.ToolCalls, false)
	}
	return fiber.Map{"id": id, "object": "chat.completion", "created": created, "model": servedOr(res.Model, model),
		"choices": []fiber.Map{{"index": 0, "message": msg, "finish_reason": finishOr(res)}}, "usage": usage(res.Usage)}
}

func wireToolCalls(calls []llm.ToolCall, indexed bool) []fiber.Map {
	out := make([]fiber.Map, len(calls))
	for i, tc := range calls {
		out[i] = fiber.Map{"id": tc.ID, "type": "function", "function": fiber.Map{"name": tc.Name, "arguments": tc.Arguments}}
		if indexed {
			out[i]["index"] = i
		}
	}
	return out
}

func usage(u llm.Usage) fiber.Map {
	return fiber.Map{"prompt_tokens": u.InputTokens, "completion_tokens": u.OutputTokens, "total_tokens": u.InputTokens + u.OutputTokens}
}

func servedOr(served, requested string) string {
	if served != "" {
		return served
	}
	return requested
}

func finishOr(res llm.Result) string {
	switch {
	case len(res.ToolCalls) > 0:
		return "tool_calls"
	case res.FinishReason != "":
		return res.FinishReason
	}
	return "stop"
}

type streamOutcome struct {
	res llm.Result
	err error
}

func (sv *Server) streamChat(c fiber.Ctx, call llm.Call, id string, created int64, model string, includeUsage bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan llm.Event, 256)
	done := make(chan streamOutcome, 1)
	// ast-grep-ignore: no-goroutine-in-fiber-handler
	go func() {
		res, err := sv.GW.Stream(ctx, call, func(ev llm.Event) {
			select {
			case events <- ev:
			case <-ctx.Done():
			}
		})
		done <- streamOutcome{res, err}
	}()
	var first *llm.Event
	select {
	case ev := <-events:
		first = &ev
	case out := <-done:
		if out.err != nil {
			cancel()
			return failErr(c, out.err)
		}
		done <- out
	}
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		write := func(v any) bool {
			raw, _ := json.Marshal(v)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
				return false
			}
			return w.Flush() == nil
		}
		chunk := func(delta fiber.Map, finish any) fiber.Map {
			return fiber.Map{"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []fiber.Map{{"index": 0, "delta": delta, "finish_reason": finish}}}
		}
		send := func(ev llm.Event) bool {
			switch ev.Kind {
			case llm.EventReasoning:
				return write(chunk(fiber.Map{"reasoning_content": ev.Text}, nil))
			case llm.EventText:
				return write(chunk(fiber.Map{"content": ev.Text}, nil))
			}
			return true
		}
		if !write(chunk(fiber.Map{"role": "assistant", "content": ""}, nil)) {
			return
		}
		if first != nil && !send(*first) {
			return
		}
		for {
			select {
			case ev := <-events:
				if !send(ev) {
					return
				}
			case out := <-done:
				for drained := false; !drained; {
					select {
					case ev := <-events:
						if !send(ev) {
							return
						}
					default:
						drained = true
					}
				}
				if out.err != nil {
					write(fiber.Map{"error": fiber.Map{"message": out.err.Error(), "type": "api_error", "code": string(llm.KindOf(out.err))}})
					_, _ = w.WriteString("data: [DONE]\n\n")
					_ = w.Flush()
					return
				}
				if len(out.res.ToolCalls) > 0 && !write(chunk(fiber.Map{"tool_calls": wireToolCalls(out.res.ToolCalls, true)}, nil)) {
					return
				}
				last := chunk(fiber.Map{}, finishOr(out.res))
				last["model"] = servedOr(out.res.Model, model)
				if !write(last) {
					return
				}
				if includeUsage {
					write(fiber.Map{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []fiber.Map{}, "usage": usage(out.res.Usage)})
				}
				_, _ = w.WriteString("data: [DONE]\n\n")
				_ = w.Flush()
				return
			}
		}
	})
}
