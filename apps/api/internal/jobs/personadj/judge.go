package personadj

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type paceLimiter struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func newPaceLimiter(rpm int) *paceLimiter {
	if rpm <= 0 {
		return nil
	}
	return &paceLimiter{gap: time.Minute / time.Duration(rpm)}
}

func (p *paceLimiter) wait(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	now := time.Now()
	slot := p.next
	if slot.Before(now) {
		slot = now
	}
	p.next = slot.Add(p.gap)
	p.mu.Unlock()
	d := time.Until(slot)
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type BatchJudge interface {
	JudgeBatch(ctx context.Context, ps []Packet) ([]Verdict, error)
}

type Judge interface {
	Judge(ctx context.Context, p Packet) (Verdict, error)
}

type HTTPJudge struct {
	baseURL   string
	token     string
	model     string
	maxTokens int
	http      *http.Client
	limiter   *paceLimiter
}

func NewHTTPJudge(baseURL, token, model string, maxTokens, rpm int) *HTTPJudge {
	if maxTokens <= 0 {
		maxTokens = 24576
	}
	return &HTTPJudge{
		limiter:   newPaceLimiter(rpm),
		baseURL:   strings.TrimRight(baseURL, "/"),
		token:     token,
		model:     model,
		maxTokens: maxTokens,
		http:      &http.Client{Timeout: 900 * time.Second},
	}
}

func (j *HTTPJudge) Configured() bool { return j.baseURL != "" && j.token != "" }

// WithRequestTimeout bounds one gateway call. The 900s default was sized for
// glm's long thinking; on 2026-09-17 the ksm.moe gateway held a deepseek call
// open with no response, and a single-worker batch sat on it for the whole
// window, so a scheduled caller sets this to a few multiples of a normal call.
func (j *HTTPJudge) WithRequestTimeout(d time.Duration) *HTTPJudge {
	if d > 0 {
		j.http.Timeout = d
	}
	return j
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type verdictJSON struct {
	Verdict       string   `json:"verdict"`
	Confidence    float64  `json:"confidence"`
	EntityKind    string   `json:"entity_kind"`
	DetachSources []string `json:"detach_sources"`
	Reason        string   `json:"reason"`
}

var retrySchedule = []time.Duration{5 * time.Second, 20 * time.Second, 45 * time.Second,
	70 * time.Second, 90 * time.Second, 120 * time.Second}

func (j *HTTPJudge) Judge(ctx context.Context, p Packet) (Verdict, error) {
	content, model, err := j.chat(ctx, SystemPrompt(p.Bucket), p.User)
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(p, content, model)
}

func parseVerdict(p Packet, content, model string) (Verdict, error) {
	var vj verdictJSON
	if err := json.Unmarshal([]byte(stripFence(content)), &vj); err != nil {
		return Verdict{}, fmt.Errorf("decode verdict: %w (body: %s)", err, truncate(content, 300))
	}
	v := strings.ToLower(strings.TrimSpace(vj.Verdict))
	if !validVerdict(v) {
		return Verdict{}, fmt.Errorf("model returned invalid verdict %q", vj.Verdict)
	}
	out := Verdict{
		Key:        p.Key,
		Bucket:     p.Bucket,
		Verdict:    v,
		Confidence: vj.Confidence,
		Reason:     strings.TrimSpace(vj.Reason),
		Model:      model,
	}
	if p.Bucket != BucketCharacterCV {
		out.EntityKind = normalizeKind(vj.EntityKind)
	}
	if p.Bucket == BucketE4Split {
		for _, s := range vj.DetachSources {
			if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
				out.DetachSources = append(out.DetachSources, s)
			}
		}
	}
	return out, nil
}

func (j *HTTPJudge) JudgeBatch(ctx context.Context, ps []Packet) ([]Verdict, error) {
	if len(ps) == 0 {
		return nil, nil
	}
	if len(ps) == 1 {
		v, err := j.Judge(ctx, ps[0])
		if err != nil {
			return nil, err
		}
		return []Verdict{v}, nil
	}
	var sb strings.Builder
	for i, p := range ps {
		fmt.Fprintf(&sb, "### 案例 %d\n%s\n\n", i+1, p.User)
	}
	content, model, err := j.chat(ctx, BatchSystemPrompt(ps[0].Bucket), sb.String())
	if err != nil {
		return nil, err
	}
	return parseBatch(ps, content, model)
}

func parseBatch(ps []Packet, content, model string) ([]Verdict, error) {
	var items []struct {
		ID int `json:"id"`
		verdictJSON
	}
	if err := json.Unmarshal([]byte(stripArrayFence(content)), &items); err != nil {
		return nil, fmt.Errorf("decode batch verdicts: %w (body: %s)", err, truncate(content, 300))
	}
	if len(items) != len(ps) {
		return nil, fmt.Errorf("batch returned %d verdicts for %d cases", len(items), len(ps))
	}
	out := make([]Verdict, 0, len(ps))
	for i, it := range items {
		if it.ID != i+1 {
			return nil, fmt.Errorf("batch item %d carries id %d — refusing a misaligned chunk", i+1, it.ID)
		}
		raw, err := json.Marshal(it.verdictJSON)
		if err != nil {
			return nil, err
		}
		v, err := parseVerdict(ps[i], string(raw), model)
		if err != nil {
			return nil, fmt.Errorf("batch item %d: %w", i+1, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func (j *HTTPJudge) chat(ctx context.Context, system, user string) (string, string, error) {
	raw, err := json.Marshal(chatRequest{
		Model:       j.model,
		MaxTokens:   j.maxTokens,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", "", err
	}
	data, err := j.post(ctx, raw)
	if err != nil {
		return "", "", err
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", "", fmt.Errorf("decode chat response: %w (body: %s)", err, truncate(string(data), 300))
	}
	if cr.Error != nil {
		return "", "", fmt.Errorf("gateway error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", "", fmt.Errorf("gateway returned no choices")
	}
	if fr := cr.Choices[0].FinishReason; fr != "" && fr != "stop" {
		return "", "", fmt.Errorf("generation finished with finish_reason=%q — refusing partial output", fr)
	}
	model := cr.Model
	if model == "" {
		model = j.model
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), model, nil
}

func (j *HTTPJudge) post(ctx context.Context, raw []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := j.limiter.wait(ctx); err != nil {
			return nil, err
		}
		data, retryable, err := j.postOnce(ctx, raw)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if !retryable || attempt >= len(retrySchedule) {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retrySchedule[attempt]):
		}
	}
}

func (j *HTTPJudge) postOnce(ctx context.Context, raw []byte) (body []byte, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+j.token)
	resp, err := j.http.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("gateway http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("gateway http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	return data, false, nil
}

type MockJudge struct {
	Rule func(Packet) (string, float64)
}

func (m MockJudge) Judge(_ context.Context, p Packet) (Verdict, error) {
	verdict, conf := VerdictUnsure, 0.5
	if m.Rule != nil {
		verdict, conf = m.Rule(p)
	}
	if !validVerdict(verdict) {
		return Verdict{}, fmt.Errorf("mock produced invalid verdict %q", verdict)
	}
	v := Verdict{
		Key: p.Key, Bucket: p.Bucket, Verdict: verdict, Confidence: conf,
		Reason: "mock:确定性判定", Model: "mock:" + promptVersion,
	}
	if p.Bucket != BucketCharacterCV {
		v.EntityKind = KindPerson
	}
	return v, nil
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	if strings.HasPrefix(s, "{") {
		return s
	}
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func stripArrayFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	if strings.HasPrefix(s, "[") {
		return s
	}
	if i, j := strings.Index(s, "["), strings.LastIndex(s, "]"); i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
