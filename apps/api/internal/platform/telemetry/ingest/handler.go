package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"api/internal/platform/telemetry/otlp"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgconn"
)

type Writer interface {
	Write(ctx context.Context, appID int64, receivedAt time.Time, batch otlp.Batch) error
}

type minuteCounters struct {
	accepted int
	rejected int
	byStatus map[int]int
}

type Handler struct {
	keys         KeyLookup
	limiter      *Limiter
	writer       Writer
	clock        Clock
	log          *slog.Logger
	writeSem     chan struct{}
	slotWait     time.Duration
	writeTimeout time.Duration
	mu           sync.Mutex
	stats        map[int64]*minuteCounters
	statsMinute  time.Time
	lastRead     atomic.Int64
}

func NewHandler(keys KeyLookup, limiter *Limiter, writer Writer, clock Clock, log *slog.Logger) *Handler {
	if clock == nil {
		clock = sysClock{}
	}
	if log == nil {
		log = slog.Default()
	}
	if limiter == nil {
		limiter = NewLimiter(clock)
	}
	return &Handler{
		keys:         keys,
		limiter:      limiter,
		writer:       writer,
		clock:        clock,
		log:          log,
		writeSem:     make(chan struct{}, 8),
		slotWait:     2 * time.Second,
		writeTimeout: 10 * time.Second,
		stats:        make(map[int64]*minuteCounters),
	}
}

func (h *Handler) Logs(c fiber.Ctx) error {
	now := h.clock.Now()
	h.flushIfNeeded(now)

	key := c.Get("x-telemetry-key")
	if key == "" {
		return h.err(c, 0, fiber.StatusUnauthorized, "missing ingest key", 0)
	}
	app, result := h.keys.Lookup(key)
	switch result {
	case LookupNotReady:
		return h.err(c, 0, fiber.StatusServiceUnavailable, "ingest not ready", 30)
	case LookupFound:
	default:
		return h.err(c, 0, fiber.StatusUnauthorized, "unknown ingest key", 0)
	}
	if !app.Enabled {
		return h.finish(c, app.ID, fiber.StatusForbidden, `{"message":"app disabled"}`, 0, 0, 0)
	}

	lim := h.limiter.Allow(app.ID, c.IP())
	if !lim.OK {
		msg := "rate limited"
		if lim.Status == fiber.StatusServiceUnavailable {
			msg = "app overloaded"
		}
		return h.finish(c, app.ID, lim.Status, `{"message":"`+msg+`"}`, lim.RetryAfter, 0, 0)
	}

	if !mediaTypeJSON(c.Get("Content-Type")) {
		return h.finish(c, app.ID, fiber.StatusUnsupportedMediaType, `{"message":"content type must be application/json"}`, 0, 0, 0)
	}
	if !encodingOK(c.Get("Content-Encoding")) {
		return h.finish(c, app.ID, fiber.StatusUnsupportedMediaType, `{"message":"content encoding must be gzip or identity"}`, 0, 0, 0)
	}

	body, nRead, status, msg := readJSONBody(c)
	h.lastRead.Store(nRead)
	if status != 0 {
		return h.finish(c, app.ID, status, `{"message":"`+msg+`"}`, 0, 0, 0)
	}

	req, err := otlp.Decode(body)
	if err != nil {
		return h.finish(c, app.ID, fiber.StatusBadRequest, `{"message":"undecodable body"}`, 0, 0, 0)
	}

	batch := otlp.Normalise(req, app.ServiceName, now)
	if len(batch.Records) == 0 {
		return h.success(c, app.ID, batch)
	}

	slotCtx, cancelSlot := context.WithTimeout(c.Context(), h.slotWait)
	defer cancelSlot()
	select {
	case h.writeSem <- struct{}{}:
		defer func() { <-h.writeSem }()
	case <-slotCtx.Done():
		h.log.Error("telemetry write slot timeout", "app_id", app.ID)
		return h.finish(c, app.ID, fiber.StatusServiceUnavailable, `{"message":"write overloaded"}`, 30, batch.Rejected, 0)
	}

	ctx, cancel := context.WithTimeout(c.Context(), h.writeTimeout)
	defer cancel()
	if err := h.writer.Write(ctx, app.ID, now, batch); err != nil {
		if sqlstate, ok := dataExceptionSQLSTATE(err); ok {
			h.log.Error("telemetry unstorable batch", "app_id", app.ID, "sqlstate", sqlstate)
			return h.finish(c, app.ID, fiber.StatusBadRequest, `{"message":"unstorable batch"}`, 0, batch.Rejected, 0)
		}
		h.log.Error("telemetry write failed", "app_id", app.ID, "err", err)
		return h.finish(c, app.ID, fiber.StatusServiceUnavailable, `{"message":"write failed"}`, 30, batch.Rejected, 0)
	}
	return h.success(c, app.ID, batch)
}

func dataExceptionSQLSTATE(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}
	if len(pgErr.Code) < 2 || pgErr.Code[:2] != "22" {
		return "", false
	}
	return pgErr.Code, true
}

type partialSuccess struct {
	RejectedLogRecords string `json:"rejectedLogRecords"`
	ErrorMessage       string `json:"errorMessage"`
}

type successBody struct {
	PartialSuccess *partialSuccess `json:"partialSuccess,omitempty"`
}

func (h *Handler) success(c fiber.Ctx, appID int64, batch otlp.Batch) error {
	c.Set("Content-Type", "application/json")
	c.Status(fiber.StatusOK)
	if batch.Rejected == 0 {
		h.note(appID, fiber.StatusOK, len(batch.Records), 0)
		return c.SendString("{}")
	}
	reason := batch.Reason
	if reason == "" {
		reason = "records rejected"
	}
	payload, err := json.Marshal(successBody{PartialSuccess: &partialSuccess{
		RejectedLogRecords: strconv.FormatInt(int64(batch.Rejected), 10),
		ErrorMessage:       reason,
	}})
	if err != nil {
		return h.finish(c, appID, fiber.StatusServiceUnavailable, `{"message":"encode failed"}`, 30, batch.Rejected, 0)
	}
	h.note(appID, fiber.StatusOK, len(batch.Records), batch.Rejected)
	return c.Send(payload)
}

func (h *Handler) err(c fiber.Ctx, appID int64, status int, msg string, retryAfter int) error {
	return h.finish(c, appID, status, `{"message":"`+msg+`"}`, retryAfter, 0, 0)
}

func (h *Handler) finish(c fiber.Ctx, appID int64, status int, body string, retryAfter, rejected, accepted int) error {
	drainRequest(c, int64(maxBody)*8)
	if retryAfter > 0 {
		c.Set("Retry-After", strconv.Itoa(retryAfter))
	}
	c.Set("Content-Type", "application/json")
	if appID != 0 {
		h.note(appID, status, accepted, rejected)
	}
	c.Status(status)
	return c.SendString(body)
}

func (h *Handler) note(appID int64, status, accepted, rejected int) {
	if appID == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	mc := h.stats[appID]
	if mc == nil {
		mc = &minuteCounters{byStatus: map[int]int{}}
		h.stats[appID] = mc
	}
	mc.accepted += accepted
	mc.rejected += rejected
	mc.byStatus[status]++
}

func (h *Handler) flushIfNeeded(now time.Time) {
	minute := now.UTC().Truncate(time.Minute)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.statsMinute.IsZero() {
		h.statsMinute = minute
		return
	}
	if minute.Equal(h.statsMinute) {
		return
	}
	h.flushLocked()
	h.statsMinute = minute
}

func (h *Handler) flushMetrics() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flushLocked()
}

func (h *Handler) flushLocked() {
	for appID, mc := range h.stats {
		attrs := []any{"app_id", appID, "accepted", mc.accepted, "rejected", mc.rejected}
		for status, n := range mc.byStatus {
			attrs = append(attrs, "status_"+strconv.Itoa(status), n)
		}
		h.log.Info("telemetry ingest", attrs...)
	}
	h.stats = make(map[int64]*minuteCounters)
}
