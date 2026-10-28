package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Request struct {
	Scene  string
	Model  string
	Body   map[string]json.RawMessage
	Site   string
	UserID int64
}

type Response struct {
	Body   []byte
	Model  string
	CallID string
	Usage  Usage
}

func (g *Gateway) Embed(ctx context.Context, r Request) (Response, error) {
	return g.unary(ctx, r, ModelEmbedding, "/embeddings")
}

func (g *Gateway) Moderate(ctx context.Context, r Request) (Response, error) {
	return g.unary(ctx, r, ModelModeration, "/moderations")
}

func (g *Gateway) unary(ctx context.Context, r Request, kind, path string) (Response, error) {
	s := g.snap.Load()
	if s == nil {
		return Response{}, &Error{Kind: ErrKindNoRoute, Msg: "gateway has no snapshot"}
	}
	sc, m, err := s.target(r.Scene, r.Model)
	if err != nil {
		return Response{}, err
	}
	if m != nil && m.kind() != kind {
		return Response{}, &Error{Kind: ErrKindBadRequest, Msg: fmt.Sprintf("model %s is a %s model, not %s", m.Key, m.Kind, kind)}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(sc.TotalMs)*time.Millisecond)
	defer cancel()
	callID := uuid.NewString()
	plan := s.plan(sc, "", g.health)
	if len(plan) == 0 {
		return Response{}, &Error{Kind: ErrKindNoRoute, Msg: "no active route for " + sc.Key}
	}
	var lastErr error
	for i, c := range plan {
		if i >= maxAttempts {
			break
		}
		res, err := g.unaryAttempt(ctx, s, sc, c, r, path, callID, true)
		if err == nil {
			res.CallID = callID
			return res, nil
		}
		lastErr = err
		if !KindOf(err).failsOver() || ctx.Err() != nil {
			return res, err
		}
	}
	return Response{CallID: callID}, lastErr
}

func (g *Gateway) unaryAttempt(ctx context.Context, s *snapshot, sc *Scene, c candidate, r Request, path, callID string, track bool) (Response, error) {
	body := map[string]any{}
	for k, v := range r.Body {
		body[k] = v
	}
	body["model"] = c.route.UpstreamID
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, &Error{Kind: ErrKindBadRequest, Msg: err.Error()}
	}
	started := g.now()
	out, resp, err := postJSON(ctx, g.client, c.provider.BaseURL+path, s.keys[c.provider.ID], s.headers[c.provider.ID], raw)
	out.firstByte = g.now().Sub(started)
	g.record(s, sc, c, Call{Site: r.Site, UserID: r.UserID}, callID, started, out, err)
	if !track {
		return resp, err
	}
	if err != nil {
		if g.health.failure(c.route.ID, KindOf(err)) {
			g.alert(c.route, err)
		}
		return resp, err
	}
	g.health.success(c.route.ID)
	return resp, nil
}

func postJSON(ctx context.Context, client *http.Client, url, key string, headers map[string]string, body []byte) (attemptOutcome, Response, error) {
	var out attemptOutcome
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/"), bytes.NewReader(body))
	if err != nil {
		return out, Response{}, &Error{Kind: ErrKindBadRequest, Msg: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, Response{}, classifyTransport(ctx, err)
	}
	defer resp.Body.Close()
	out.status = resp.StatusCode
	out.requestID = resp.Header.Get("x-request-id")
	if out.requestID == "" {
		out.requestID = resp.Header.Get("cf-ray")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, sseBodyCap))
	if err != nil {
		return out, Response{}, classifyTransport(ctx, err)
	}
	if resp.StatusCode != http.StatusOK {
		return out, Response{}, classifyHTTP(resp.StatusCode, string(data))
	}
	var meta struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return out, Response{}, &Error{Kind: ErrKindMalformed, Status: resp.StatusCode, Msg: truncate(string(data), 300)}
	}
	in := meta.Usage.PromptTokens
	if in == 0 {
		in = meta.Usage.TotalTokens
	}
	out.result = Result{Model: meta.Model, Usage: Usage{InputTokens: in}}
	return out, Response{Body: data, Model: meta.Model, Usage: out.result.Usage}, nil
}

type ProbeResult struct {
	OK          bool   `json:"ok"`
	LatencyMs   int    `json:"latency_ms"`
	ServedModel string `json:"served_model"`
	ErrorKind   string `json:"error_kind"`
	Message     string `json:"message"`
}

func (g *Gateway) Probe(ctx context.Context, routeID int64) (ProbeResult, error) {
	var (
		r Route
		p Provider
		m Model
	)
	if err := g.db.WithContext(ctx).First(&r, routeID).Error; err != nil {
		return ProbeResult{}, err
	}
	if err := g.db.WithContext(ctx).First(&p, r.ProviderID).Error; err != nil {
		return ProbeResult{}, err
	}
	if err := g.db.WithContext(ctx).First(&m, r.ModelID).Error; err != nil {
		return ProbeResult{}, err
	}
	key, err := providerKey(&p, g.kek)
	if err != nil {
		return ProbeResult{ErrorKind: string(ErrKindAuth), Message: err.Error()}, nil
	}
	var headers map[string]string
	_ = json.Unmarshal(p.Headers, &headers)
	s := &snapshot{keys: map[int64]string{p.ID: key}, headers: map[int64]map[string]string{p.ID: headers}, prices: map[int64]*PriceRevision{}}
	sc := &Scene{Key: "admin.test", ModelID: m.ID, Class: ClassBatch, TTFTMs: 20_000, IdleMs: 20_000, TotalMs: 30_000}
	c := candidate{route: &r, model: &m, provider: &p}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := g.now()
	var served string
	switch m.kind() {
	case ModelEmbedding:
		var res Response
		res, err = g.unaryAttempt(ctx, s, sc, c, Request{Body: map[string]json.RawMessage{"input": json.RawMessage(`["ping"]`)}, Site: "admin"}, "/embeddings", uuid.NewString(), false)
		served = res.Model
	case ModelModeration:
		var res Response
		res, err = g.unaryAttempt(ctx, s, sc, c, Request{Body: map[string]json.RawMessage{"input": json.RawMessage(`"ping"`)}, Site: "admin"}, "/moderations", uuid.NewString(), false)
		served = res.Model
	default:
		var res Result
		res, _, err = g.attempt(ctx, s, sc, c, Call{Messages: []Message{{Role: RoleUser, Content: "ping"}}, MaxTokens: 16, Buffered: true, Site: "admin", probe: true}, uuid.NewString(), nil)
		served = res.Model
	}
	out := ProbeResult{OK: err == nil, LatencyMs: int(g.now().Sub(started) / time.Millisecond), ServedModel: served}
	if err != nil {
		out.ErrorKind, out.Message = string(KindOf(err)), err.Error()
	}
	return out, nil
}

func HashAccessKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (g *Gateway) Authorize(secret string) (*AccessKey, bool) {
	s := g.snap.Load()
	if s == nil || secret == "" {
		return nil, false
	}
	k := s.access[HashAccessKey(secret)]
	return k, k != nil
}

func (g *Gateway) Allows(k *AccessKey, name string) bool {
	var allowed []string
	if json.Unmarshal(k.AllowedModels, &allowed) != nil || len(allowed) == 0 {
		return true
	}
	if slices.Contains(allowed, name) {
		return true
	}
	s := g.snap.Load()
	if s == nil {
		return false
	}
	if sc := s.scenes[name]; sc != nil {
		if m := s.models[sc.ModelID]; m != nil && slices.Contains(allowed, m.Key) {
			return true
		}
	}
	if id, ok := s.aliases[name]; ok {
		if m := s.models[id]; m != nil && slices.Contains(allowed, m.Key) {
			return true
		}
	}
	return false
}

type usageMarker struct {
	mu   sync.Mutex
	last map[int64]time.Time
}

func (g *Gateway) MarkUsed(k *AccessKey) {
	now := g.now()
	g.used.mu.Lock()
	if t, ok := g.used.last[k.ID]; ok && now.Sub(t) < time.Minute {
		g.used.mu.Unlock()
		return
	}
	g.used.last[k.ID] = now
	g.used.mu.Unlock()
	go func() {
		_ = g.db.Model(&AccessKey{}).Where("id = ?", k.ID).Update("last_used_at", now).Error
	}()
}

func (g *Gateway) Ready(scene string) (string, bool) {
	s := g.snap.Load()
	if s == nil || s.scenes[scene] == nil {
		return "", false
	}
	sc := s.scenes[scene]
	key := ""
	if m := s.models[sc.ModelID]; m != nil {
		key = m.Key
	}
	return key, len(s.plan(sc, "", g.health)) > 0
}

func (g *Gateway) Models() []Model {
	s := g.snap.Load()
	if s == nil {
		return nil
	}
	out := make([]Model, 0, len(s.models))
	for _, m := range s.models {
		if len(s.routes[m.ID]) > 0 {
			out = append(out, *m)
		}
	}
	slices.SortFunc(out, func(a, b Model) int { return strings.Compare(a.Key, b.Key) })
	return out
}
