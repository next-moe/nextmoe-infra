package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	sseLineCap = 1 << 20
	sseBodyCap = 32 << 20
)

type chatTarget struct {
	baseURL  string
	key      string
	headers  map[string]string
	upstream string
}

type chatLimits struct {
	ttft, idle time.Duration
}

type chatSink struct {
	delta     func(kind EventKind, text string)
	toolStart func()
}

type attemptOutcome struct {
	result    Result
	status    int
	requestID string
	firstByte time.Duration
}

func chatRequestBody(c Call, upstream string, temperature *float64, maxTokens int) ([]byte, error) {
	type fn struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type tc struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}
	type msg struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCalls  []tc            `json:"tool_calls,omitempty"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
	}
	msgs := make([]msg, len(c.Messages))
	for i, m := range c.Messages {
		content := m.Raw
		if len(content) == 0 {
			content, _ = json.Marshal(m.Content)
		}
		msgs[i] = msg{Role: string(m.Role), Content: content, ToolCallID: m.ToolCallID}
		for _, call := range m.ToolCalls {
			msgs[i].ToolCalls = append(msgs[i].ToolCalls, tc{ID: call.ID, Type: "function", Function: fn{Name: call.Name, Arguments: call.Arguments}})
		}
	}
	body := map[string]any{
		"model":          upstream,
		"messages":       msgs,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if maxTokens > 0 {
		body["max_tokens"] = maxTokens
	}
	if temperature != nil {
		body["temperature"] = *temperature
	}
	if len(c.Tools) > 0 {
		tools := make([]map[string]any, len(c.Tools))
		for i, t := range c.Tools {
			params := t.Parameters
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools[i] = map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": params,
			}}
		}
		body["tools"] = tools
		if c.ToolChoice != "" {
			body["tool_choice"] = c.ToolChoice
		}
	}
	if c.JSON {
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	for k, v := range c.Extra {
		if !reservedBodyKeys[k] {
			body[k] = v
		}
	}
	return json.Marshal(body)
}

var reservedBodyKeys = map[string]bool{"model": true, "messages": true, "stream": true, "stream_options": true, "tools": true, "input": true}

type chunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content          lenientText `json:"content"`
			ReasoningContent lenientText `json:"reasoning_content"`
			Reasoning        lenientText `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string      `json:"name"`
					Arguments lenientText `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		Message *struct {
			Content          lenientText `json:"content"`
			ReasoningContent lenientText `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
		CostInUSDTicks int64 `json:"cost_in_usd_ticks"`
		CostInNanoUSD  int64 `json:"cost_in_nano_usd"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Workers AI's qwen3-30b-a3b streamed a digit token as a bare JSON number,
// {"delta":{"content":1}}; decoding into a string failed the frame and every
// answer that listed "1." died mid-stream as malformed.
type lenientText string

func (a *lenientText) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*a = lenientText(s)
		return nil
	}
	if string(b) == "null" {
		return nil
	}
	*a = lenientText(b)
	return nil
}

type accumulator struct {
	text, reasoning strings.Builder
	tools           map[int]*ToolCall
	result          Result
	finished        bool
}

func (a *accumulator) apply(c *chunk, sink chatSink) {
	if c.Model != "" {
		a.result.Model = c.Model
	}
	if c.Usage != nil {
		a.result.Usage.InputTokens = c.Usage.PromptTokens
		a.result.Usage.OutputTokens = c.Usage.CompletionTokens
		if d := c.Usage.PromptTokensDetails; d != nil {
			a.result.Usage.CachedTokens = d.CachedTokens
		}
		if d := c.Usage.CompletionTokensDetails; d != nil {
			a.result.Usage.ReasoningTokens = d.ReasoningTokens
		}
		a.result.Usage.ReportedNano = reportedNano(c.Usage.CostInUSDTicks, c.Usage.CostInNanoUSD)
	}
	for _, ch := range c.Choices {
		d := ch.Delta
		content, reasoning := string(d.Content), string(d.ReasoningContent)
		if reasoning == "" {
			reasoning = string(d.Reasoning)
		}
		if ch.Message != nil {
			content += string(ch.Message.Content)
			reasoning += string(ch.Message.ReasoningContent)
		}
		if reasoning != "" {
			a.reasoning.WriteString(reasoning)
			sink.delta(EventReasoning, reasoning)
		}
		if content != "" {
			a.text.WriteString(content)
			sink.delta(EventText, content)
		}
		for _, t := range d.ToolCalls {
			cur := a.tools[t.Index]
			if cur == nil {
				cur = &ToolCall{}
				a.tools[t.Index] = cur
				sink.toolStart()
			}
			if t.ID != "" {
				cur.ID = t.ID
			}
			cur.Name += t.Function.Name
			cur.Arguments += string(t.Function.Arguments)
		}
		if ch.FinishReason != "" {
			a.result.FinishReason = ch.FinishReason
			a.finished = true
		}
	}
}

func (a *accumulator) finish() Result {
	a.result.Text = a.text.String()
	a.result.Reasoning = a.reasoning.String()
	idx := make([]int, 0, len(a.tools))
	for i := range a.tools {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		a.result.ToolCalls = append(a.result.ToolCalls, *a.tools[i])
	}
	return a.result
}

func streamChat(ctx context.Context, client *http.Client, t chatTarget, body []byte, lim chatLimits, sink chatSink) (attemptOutcome, error) {
	var out attemptOutcome
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	watchdog := time.AfterFunc(lim.ttft, func() { stalled.Store(true); cancel() })
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(t.baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return out, &Error{Kind: ErrKindBadRequest, Msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if t.key != "" {
		req.Header.Set("Authorization", "Bearer "+t.key)
	}
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		if stalled.Load() {
			return out, &Error{Kind: ErrKindTimeout, Msg: "no response before the first-token deadline"}
		}
		return out, classifyTransport(ctx, err)
	}
	defer resp.Body.Close()
	out.status = resp.StatusCode
	out.requestID = resp.Header.Get("Cf-Ray")
	if out.requestID == "" {
		out.requestID = resp.Header.Get("X-Request-Id")
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return out, httpError(resp, string(raw))
	}

	acc := &accumulator{tools: map[int]*ToolCall{}}
	body2 := io.LimitReader(resp.Body, sseBodyCap)
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		raw, err := io.ReadAll(body2)
		if err != nil {
			return out, classifyTransport(ctx, err)
		}
		out.firstByte = time.Since(start)
		var c chunk
		if err := json.Unmarshal(raw, &c); err != nil {
			return out, &Error{Kind: ErrKindMalformed, Msg: err.Error()}
		}
		acc.apply(&c, sink)
		acc.finished = true
		out.result = acc.finish()
		return out, nil
	}

	rd := bufio.NewReaderSize(body2, 64<<10)
	for {
		line, err := readLine(rd)
		if err != nil {
			if err == io.EOF {
				break
			}
			if stalled.Load() {
				return out, &Error{Kind: ErrKindTimeout, Msg: "stream stalled"}
			}
			return out, classifyTransport(ctx, err)
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}
		if out.firstByte == 0 {
			out.firstByte = time.Since(start)
		}
		watchdog.Reset(lim.idle)
		if data == "[DONE]" {
			acc.finished = true
			break
		}
		var c chunk
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return out, &Error{Kind: ErrKindMalformed, Msg: "bad stream frame (" + err.Error() + "): " + truncate(data, 400)}
		}
		if c.Error != nil {
			return out, classifyHTTP(http.StatusBadGateway, c.Error.Message)
		}
		acc.apply(&c, sink)
	}
	if !acc.finished {
		return out, &Error{Kind: ErrKindNetwork, Msg: "stream ended without a finish signal"}
	}
	out.result = acc.finish()
	if out.result.FinishReason == "content_filter" {
		return out, &Error{Kind: ErrKindContentFiltered, Msg: "finish_reason content_filter"}
	}
	return out, nil
}

func readLine(rd *bufio.Reader) (string, error) {
	var buf []byte
	for {
		part, isPrefix, err := rd.ReadLine()
		if err != nil {
			return "", err
		}
		buf = append(buf, part...)
		if len(buf) > sseLineCap {
			return "", &Error{Kind: ErrKindMalformed, Msg: "stream line over 1 MiB"}
		}
		if !isPrefix {
			return string(buf), nil
		}
	}
}
