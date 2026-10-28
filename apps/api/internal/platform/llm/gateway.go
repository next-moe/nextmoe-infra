package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	maxAttempts     = 6
	refreshInterval = 30 * time.Second
	coolSync        = 2 * time.Second
)

var (
	ErrUnknownScene = errors.New("llm: unknown scene")
	ErrUnknownModel = errors.New("llm: unknown model")
)

type Gateway struct {
	db     *gorm.DB
	kek    []byte
	client *http.Client
	health *health
	coord  Coordinator
	stats  *routeStats
	snap   atomic.Pointer[snapshot]
	used   usageMarker
	now    func() time.Time
}

func New(db *gorm.DB, kek []byte) *Gateway {
	g := &Gateway{
		db:  db,
		kek: kek,
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		now:  time.Now,
		used: usageMarker{last: map[int64]time.Time{}},
	}
	g.health = newHealth(func() time.Time { return g.now() })
	g.coord = newLocalCoord(func() time.Time { return g.now() })
	g.stats = newRouteStats()
	return g
}

func (g *Gateway) SetCoordinator(c Coordinator) { g.coord = c }

func (g *Gateway) syncCooling(ctx context.Context) {
	if m, err := g.coord.Cooling(ctx); err == nil {
		g.health.setShared(m)
	}
}

func (g *Gateway) Refresh(ctx context.Context) error {
	s, err := loadSnapshot(ctx, g.db, g.kek)
	if err != nil {
		return err
	}
	g.snap.Store(s)
	return nil
}

func (g *Gateway) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(coolSync)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				g.syncCooling(ctx)
			}
		}
	}()
	go func() {
		t := time.NewTicker(refreshInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := g.Refresh(ctx); err != nil {
					slog.Warn("llm gateway: refresh failed, keeping the previous snapshot", "err", err)
				}
			}
		}
	}()
}

func (g *Gateway) HasScene(key string) bool {
	s := g.snap.Load()
	return s != nil && s.scenes[key] != nil
}

func (g *Gateway) Stream(ctx context.Context, call Call, onEvent func(Event)) (Result, error) {
	s := g.snap.Load()
	if s == nil {
		return Result{}, &Error{Kind: ErrKindNoRoute, Msg: "gateway has no snapshot"}
	}
	sc, m, err := s.target(call.Scene, call.Model)
	if err != nil {
		return Result{}, err
	}
	if m != nil && m.kind() != ModelChat {
		return Result{}, &Error{Kind: ErrKindBadRequest, Msg: fmt.Sprintf("model %s is a %s model, not chat", m.Key, m.Kind)}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(sc.TotalMs)*time.Millisecond)
	defer cancel()
	callID := uuid.NewString()
	queue := s.plan(sc, call.AffinityKey, g.health, g.stats)
	if len(queue) == 0 {
		return Result{}, &Error{Kind: ErrKindNoRoute, Msg: "no active route for scene " + call.Scene}
	}
	var lastErr error
	skipModel := int64(0)
	attempts, limited := 0, 0
	usedContext, usedContent := false, false
	for i := 0; i < len(queue) && attempts < maxAttempts; i++ {
		c := queue[i]
		if c.model.ID == skipModel {
			continue
		}
		res, committed, err := g.attempt(ctx, s, sc, c, call, callID, onEvent)
		if err == nil {
			res.CallID = callID
			return res, nil
		}
		kind := KindOf(err)
		if kind == ErrKindLimited {
			limited++
			continue
		}
		attempts++
		lastErr = err
		if committed || ctx.Err() != nil {
			return res, err
		}
		switch {
		case kind == ErrKindContextLength:
			skipModel = c.model.ID
			if !usedContext {
				usedContext = true
				queue = append(queue, s.planChain(sc, modelIDs(sc.ContextFallback), call.AffinityKey, g.health, g.stats, false)...)
			}
		case kind == ErrKindContentFiltered && !usedContent && len(modelIDs(sc.ContentFallback)) > 0:
			usedContent = true
			queue = append(queue[:i+1], s.planChain(sc, modelIDs(sc.ContentFallback), call.AffinityKey, g.health, g.stats, false)...)
		case !kind.failsOver():
			return res, err
		}
	}
	if lastErr == nil && limited > 0 {
		return Result{CallID: callID}, &Error{Kind: ErrKindRateLimit, Msg: "every route of scene " + sc.Key + " is at its concurrency or per-minute limit"}
	}
	return Result{CallID: callID}, lastErr
}

func (g *Gateway) attempt(ctx context.Context, s *snapshot, sc *Scene, c candidate, call Call, callID string, onEvent func(Event)) (Result, bool, error) {
	maxTokens := call.MaxTokens
	if maxTokens == 0 {
		maxTokens = sc.MaxOutput
	}
	if c.model.MaxOutput > 0 && (maxTokens == 0 || maxTokens > c.model.MaxOutput) {
		maxTokens = c.model.MaxOutput
	}
	body, err := chatRequestBody(call, c.route.UpstreamID, sc.Temperature, maxTokens)
	if err != nil {
		return Result{}, false, &Error{Kind: ErrKindBadRequest, Msg: err.Error()}
	}
	release, ok := g.coord.Acquire(ctx, c.route.ID, c.route.MaxConcurrency, c.route.RPM)
	if !ok {
		return Result{}, false, &Error{Kind: ErrKindLimited, Msg: "route at its limit"}
	}
	defer release()
	defer g.stats.begin(c.route.ID)()
	committed := false
	var pending []Event
	flush := func() {
		committed = true
		for _, ev := range pending {
			onEvent(ev)
		}
		pending = nil
	}
	sink := chatSink{
		delta: func(kind EventKind, text string) {
			if call.Buffered || onEvent == nil {
				return
			}
			ev := Event{Kind: kind, Text: text}
			if committed {
				onEvent(ev)
				return
			}
			pending = append(pending, ev)
			if kind == EventText && strings.TrimSpace(text) != "" {
				flush()
			}
		},
		toolStart: func() {
			if !call.Buffered && onEvent != nil && !committed {
				flush()
			}
		},
	}
	started := g.now()
	out, err := streamChat(ctx, g.client, chatTarget{
		baseURL: c.provider.BaseURL, key: s.keys[c.provider.ID], headers: s.headers[c.provider.ID], upstream: c.route.UpstreamID,
	}, body, chatLimits{ttft: time.Duration(sc.TTFTMs) * time.Millisecond, idle: time.Duration(sc.IdleMs) * time.Millisecond}, sink)
	if err == nil && !committed && onEvent != nil && !call.Buffered {
		flush()
	}
	g.record(s, sc, c, call, callID, started, out, err)
	if call.probe {
		return out.result, committed, err
	}
	if err != nil {
		g.noteFailure(ctx, c.route, err)
		return out.result, committed, err
	}
	g.noteSuccess(ctx, c.route.ID, out.firstByte)
	return out.result, committed, nil
}

func (g *Gateway) noteFailure(ctx context.Context, r *Route, err error) {
	alert, opened := g.health.failure(r.ID, err)
	if alert {
		g.alert(r, err)
	}
	if !opened.IsZero() {
		g.coord.PublishCool(context.WithoutCancel(ctx), r.ID, opened)
	}
}

func (g *Gateway) noteSuccess(ctx context.Context, id int64, latency time.Duration) {
	g.stats.observe(id, float64(latency/time.Millisecond))
	if g.health.success(id) {
		g.coord.ClearCool(context.WithoutCancel(ctx), id)
	}
}

func (g *Gateway) record(s *snapshot, sc *Scene, c candidate, call Call, callID string, started time.Time, out attemptOutcome, err error) {
	if g.db == nil {
		return
	}
	u := out.result.Usage
	a := Attempt{
		CallID: callID, Scene: sc.Key, RouteID: c.route.ID, ProviderID: c.provider.ID, ModelKey: c.model.Key,
		ServedModel: out.result.Model, UpstreamRequestID: out.requestID, Site: call.Site, UserID: call.UserID,
		OK: err == nil, HTTPStatus: out.status, TTFTMs: int(out.firstByte / time.Millisecond),
		DurationMs: int(g.now().Sub(started) / time.Millisecond), InputTokens: u.InputTokens, CachedTokens: u.CachedTokens,
		OutputTokens: u.OutputTokens, ReasoningTokens: u.ReasoningTokens, UsageEstimated: u.Estimated,
		PriceID: c.route.PriceID, CreatedAt: g.now(),
	}
	if err != nil {
		a.ErrorKind = string(KindOf(err))
	}
	switch {
	case u.ReportedNano > 0:
		a.CostNano = scaled(u.ReportedNano, c.provider.PriceMultiplierPPM)
	case c.route.PriceID != nil:
		if p := s.prices[*c.route.PriceID]; p != nil {
			a.CostNano = costNano(u, p, c.provider.PriceMultiplierPPM)
		}
	}
	if dbErr := g.db.Create(&a).Error; dbErr != nil {
		slog.Warn("llm gateway: record attempt", "err", dbErr)
	}
}

func costNano(u Usage, p *PriceRevision, multiplierPPM int64) int64 {
	uncached := max(u.InputTokens-u.CachedTokens, 0)
	micro := int64(uncached)*p.InputMicro + int64(u.CachedTokens)*p.CachedMicro + int64(u.OutputTokens)*p.OutputMicro
	micro += p.PerCallMicro * int64(max(u.Units, 1)) * 1_000_000
	return scaled(micro, multiplierPPM) / 1000
}

func scaled(v, multiplierPPM int64) int64 {
	if multiplierPPM <= 0 {
		multiplierPPM = 1_000_000
	}
	return v * multiplierPPM / 1_000_000
}

func (g *Gateway) alert(r *Route, cause error) {
	slog.Warn("llm gateway: route keeps failing auth/quota; check its provider key or balance", "route", r.ID, "err", cause)
	if g.db == nil {
		return
	}
	_ = g.db.Create(&RouteEvent{RouteID: r.ID, ProviderID: r.ProviderID, Kind: "auth_failing", Detail: truncate(cause.Error(), 250), CreatedAt: g.now()}).Error
}
