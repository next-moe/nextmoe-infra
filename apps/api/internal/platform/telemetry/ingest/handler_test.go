package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api/internal/app"
	"api/internal/platform/telemetry/otlp"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgconn"
)

const testKey = "0123456789abcdef0123456789abcdef"

type staticKeys map[string]AppInfo

func (s staticKeys) Lookup(k string) (AppInfo, LookupResult) {
	a, ok := s[k]
	if !ok {
		return AppInfo{}, LookupUnknown
	}
	return a, LookupFound
}

type memWriter struct {
	calls int
	err   error
}

func (w *memWriter) Write(context.Context, int64, time.Time, otlp.Batch) error {
	w.calls++
	return w.err
}

func enabledApp() staticKeys {
	return staticKeys{testKey: {ID: 7, ServiceName: "kungal-app", Enabled: true, IngestKey: testKey}}
}

func validJSON() []byte {
	return []byte(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"kungal-app"}}]},"scopeLogs":[{"logRecords":[{"eventName":"session.start","timeUnixNano":"1000","attributes":[{"key":"session.id","value":{"stringValue":"0123456789abcdef0123456789abcdef"}}]}]}]}]}`)
}

func mismatchJSON() []byte {
	return []byte(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"other"}}]},"scopeLogs":[{"logRecords":[{"eventName":"session.start","timeUnixNano":"1"}]}]}]}`)
}

type harness struct {
	h      *Handler
	app    *fiber.App
	writer *memWriter
	clock  *frozenClock
	lim    *Limiter
	logBuf *bytes.Buffer
}

func newHarness(keys KeyLookup) *harness {
	clk := &frozenClock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	w := &memWriter{}
	lim := NewLimiter(clk)
	h := NewHandler(keys, lim, w, clk, log)
	fa := fiber.New(app.FiberConfig("kun-telemetry-test"))
	fa.Post("/v1/logs", h.Logs)
	return &harness{h: h, app: fa, writer: w, clock: clk, lim: lim, logBuf: &buf}
}

func (h *harness) post(t *testing.T, key, ctype, enc, ip string, body []byte, chunked bool) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if enc != "" {
		req.Header.Set("Content-Encoding", enc)
	}
	if key != "" {
		req.Header.Set("x-telemetry-key", key)
	}
	req.RemoteAddr = "10.0.0.1:1234"
	if ip != "" {
		clientIP := ip
		if host, _, err := net.SplitHostPort(ip); err == nil {
			clientIP = host
		}
		req.Header.Set("CF-Connecting-IP", clientIP)
	}
	if chunked {
		req.TransferEncoding = []string{"chunked"}
		req.Header.Del("Content-Length")
	}
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw), resp.Header
}

func (h *harness) postGZ(t *testing.T, body []byte) (int, string, http.Header) {
	t.Helper()
	return h.post(t, testKey, "application/json", "gzip", "10.0.0.1:1", gzipJSONBytes(body), false)
}

func TestUnauthorizedKey(t *testing.T) {
	h := newHarness(enabledApp())
	st, body, _ := h.post(t, "", "application/json", "gzip", "10.0.0.1:1", gzipJSONBytes(validJSON()), false)
	if st != 401 {
		t.Fatalf("missing key status=%d body=%s", st, body)
	}
	st, body, _ = h.post(t, "deadbeef", "application/json", "gzip", "10.0.0.1:1", gzipJSONBytes(validJSON()), false)
	if st != 401 {
		t.Fatalf("unknown key status=%d body=%s", st, body)
	}
}

func TestForbiddenDisabledApp(t *testing.T) {
	keys := staticKeys{testKey: {ID: 7, ServiceName: "kungal-app", Enabled: false, IngestKey: testKey}}
	h := newHarness(keys)
	st, _, _ := h.postGZ(t, validJSON())
	if st != 403 {
		t.Fatalf("status=%d", st)
	}
}

func TestRateLimitedPerIP(t *testing.T) {
	h := newHarness(enabledApp())
	gz := gzipJSONBytes(validJSON())
	ip := "10.8.0.1:1"
	for i := 0; i < 60; i++ {
		st, _, _ := h.post(t, testKey, "application/json", "gzip", ip, gz, false)
		if st != 200 {
			t.Fatalf("req %d status=%d", i, st)
		}
	}
	st, _, hdr := h.post(t, testKey, "application/json", "gzip", ip, gz, false)
	if st != 429 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "5" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
}

func TestOverloadedPerApp(t *testing.T) {
	h := newHarness(enabledApp())
	for i := 0; i < 1000; i++ {
		ip := fmt.Sprintf("10.%d.%d.1", i/256, i%256)
		if !h.lim.Allow(7, ip).OK {
			t.Fatalf("prefill %d denied", i)
		}
	}
	st, _, hdr := h.postGZ(t, validJSON())
	if st != 503 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "30" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
}

func TestUnsupportedContentType(t *testing.T) {
	h := newHarness(enabledApp())
	st, _, _ := h.post(t, testKey, "text/plain", "gzip", "10.0.0.1:1", gzipJSONBytes(validJSON()), false)
	if st != 415 {
		t.Fatalf("status=%d", st)
	}
}

func TestUnsupportedContentEncoding(t *testing.T) {
	h := newHarness(enabledApp())
	st, _, _ := h.post(t, testKey, "application/json", "deflate", "10.0.0.1:1", gzipJSONBytes(validJSON()), false)
	if st != 415 {
		t.Fatalf("status=%d", st)
	}
}

func TestRawBodyTooLarge(t *testing.T) {
	h := newHarness(enabledApp())
	body := bytes.Repeat([]byte("a"), maxBody+1)
	st, _, _ := h.post(t, testKey, "application/json", "identity", "10.0.0.1:1", body, false)
	if st != 413 {
		t.Fatalf("status=%d", st)
	}
}

func TestCorruptGzip(t *testing.T) {
	h := newHarness(enabledApp())
	st, _, _ := h.post(t, testKey, "application/json", "gzip", "10.0.0.1:1", []byte("not-gzip"), false)
	if st != 400 {
		t.Fatalf("status=%d", st)
	}
}

func TestDecompressedTooLarge(t *testing.T) {
	h := newHarness(enabledApp())
	st, _, _ := h.post(t, testKey, "application/json", "gzip", "10.0.0.1:1", gzipZeros(maxBody+1), false)
	if st != 413 {
		t.Fatalf("status=%d", st)
	}
}

func gzipZeros(n int) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(bytes.Repeat([]byte{0}, n))
	_ = w.Close()
	return buf.Bytes()
}

func TestNotDecodable(t *testing.T) {
	h := newHarness(enabledApp())
	st, _, _ := h.postGZ(t, []byte(`[]`))
	if st != 400 {
		t.Fatalf("status=%d", st)
	}
}

func TestZeroAcceptedNoWrite(t *testing.T) {
	h := newHarness(enabledApp())
	st, _, _ := h.postGZ(t, mismatchJSON())
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	if h.writer.calls != 0 {
		t.Fatalf("writer called %d times", h.writer.calls)
	}
}

func TestWriteSlotUnavailable(t *testing.T) {
	h := newHarness(enabledApp())
	h.h.slotWait = 30 * time.Millisecond
	for i := 0; i < 8; i++ {
		h.h.writeSem <- struct{}{}
	}
	st, _, hdr := h.postGZ(t, validJSON())
	if st != 503 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "30" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
}

func TestWriterError(t *testing.T) {
	h := newHarness(enabledApp())
	h.writer.err = errors.New("db down")
	st, _, hdr := h.postGZ(t, validJSON())
	if st != 503 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "30" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
}

func TestWritten(t *testing.T) {
	h := newHarness(enabledApp())
	st, body, _ := h.postGZ(t, validJSON())
	if st != 200 || body != "{}" {
		t.Fatalf("status=%d body=%q", st, body)
	}
	if h.writer.calls != 1 {
		t.Fatalf("writer calls=%d", h.writer.calls)
	}
}

func TestChunkedAccepted(t *testing.T) {
	h := newHarness(enabledApp())
	st, body, _ := h.post(t, testKey, "application/json", "gzip", "10.0.0.1:1", gzipJSONBytes(validJSON()), true)
	if st != 200 || body != "{}" {
		t.Fatalf("status=%d body=%q", st, body)
	}
}

func TestGzipBombRejected(t *testing.T) {
	h := newHarness(enabledApp())
	st, _, _ := h.post(t, testKey, "application/json", "gzip", "10.0.0.1:1", gzipZeros(maxBody+1), false)
	if st != 413 {
		t.Fatalf("status=%d", st)
	}
	if n := h.h.lastRead.Load(); n > int64(maxBody)+1 {
		t.Fatalf("reader produced %d bytes", n)
	}
}

func TestPartialSuccessBody(t *testing.T) {
	h := newHarness(enabledApp())
	raw := []byte(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"kungal-app"}}]},"scopeLogs":[{"logRecords":[{"eventName":"ok","timeUnixNano":"1"},{"timeUnixNano":"2"}]}]}]}`)
	st, body, _ := h.postGZ(t, raw)
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	want := `{"partialSuccess":{"rejectedLogRecords":"1","errorMessage":"empty eventName"}}`
	if body != want {
		t.Fatalf("body=%s want %s", body, want)
	}
}

func TestEmptyObjectOnFullSuccess(t *testing.T) {
	h := newHarness(enabledApp())
	st, body, _ := h.postGZ(t, validJSON())
	if st != 200 || body != "{}" {
		t.Fatalf("status=%d body=%q", st, body)
	}
}

func TestNoIPInLogs(t *testing.T) {
	h := newHarness(enabledApp())
	ip := "203.0.113.99:4242"
	st, _, _ := h.post(t, testKey, "application/json", "gzip", ip, gzipJSONBytes(validJSON()), false)
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	h.h.flushMetrics()
	if strings.Contains(h.logBuf.String(), "203.0.113.99") {
		t.Fatalf("IP appeared in logs: %s", h.logBuf.String())
	}
}

func TestLimiterKeysAreHashed(t *testing.T) {
	h := newHarness(enabledApp())
	ip := "203.0.113.77"
	if !h.lim.Allow(7, ip).OK {
		t.Fatal("allow")
	}
	_, _, _ = h.post(t, testKey, "application/json", "gzip", ip, gzipJSONBytes(validJSON()), false)
	for _, k := range h.lim.ipKeyStrings() {
		if strings.Contains(k, ip) {
			t.Fatalf("limiter key contains IP: %q", k)
		}
	}
	if h.lim.ipBucketCount() == 0 {
		t.Fatal("expected hashed buckets")
	}
}

func TestIdleBucketsEvicted(t *testing.T) {
	clk := &frozenClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	lim := NewLimiter(clk)
	if !lim.Allow(1, "10.0.0.1").OK {
		t.Fatal("first allow")
	}
	if lim.ipBucketCount() == 0 {
		t.Fatal("expected a bucket")
	}
	clk.Add(10 * time.Minute)
	lim.Sweep(clk.Now())
	if lim.ipBucketCount() != 0 {
		t.Fatalf("idle buckets remain: %d", lim.ipBucketCount())
	}
}

type memApps struct {
	apps  []AppInfo
	err   error
	calls int
}

func (m *memApps) ListApps(context.Context) ([]AppInfo, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	out := make([]AppInfo, len(m.apps))
	copy(out, m.apps)
	return out, nil
}

func TestKeyCacheInvalidate(t *testing.T) {
	clk := &frozenClock{t: time.Now()}
	src := &memApps{}
	c := NewKeyCache(src, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, res := c.Lookup("abc"); res != LookupNotReady {
		t.Fatalf("unloaded lookup result=%v", res)
	}
	src.apps = []AppInfo{{ID: 1, ServiceName: "a", Enabled: true, IngestKey: "abc"}}
	if _, res := c.Lookup("abc"); res == LookupFound {
		t.Fatal("cache served a key before invalidate")
	}
	if err := c.Invalidate(); err != nil {
		t.Fatal(err)
	}
	got, res := c.Lookup("abc")
	if res != LookupFound || got.ID != 1 {
		t.Fatalf("after invalidate: res=%v got=%+v", res, got)
	}
}

func TestUnstorableBatchIs400(t *testing.T) {
	h := newHarness(enabledApp())
	h.writer.err = fmt.Errorf("insert events: %w", &pgconn.PgError{Code: "22P05"})
	st, body, hdr := h.postGZ(t, validJSON())
	if st != 400 {
		t.Fatalf("status=%d body=%s", st, body)
	}
	if body != `{"message":"unstorable batch"}` {
		t.Fatalf("body=%s", body)
	}
	if hdr.Get("Retry-After") != "" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
	if strings.Contains(h.logBuf.String(), "10.0.0.1") {
		t.Fatalf("IP in logs: %s", h.logBuf.String())
	}
}

func TestOtherWriteErrorIs503(t *testing.T) {
	cases := []error{
		fmt.Errorf("insert events: %w", &pgconn.PgError{Code: "23505"}),
		fmt.Errorf("insert events: %w", &pgconn.PgError{Code: "40001"}),
		errors.New("db down"),
	}
	for i, err := range cases {
		h := newHarness(enabledApp())
		h.writer.err = err
		st, _, hdr := h.postGZ(t, validJSON())
		if st != 503 {
			t.Errorf("case %d status=%d", i, st)
		}
		if hdr.Get("Retry-After") != "30" {
			t.Errorf("case %d Retry-After=%q", i, hdr.Get("Retry-After"))
		}
	}
}

type waitWriter struct{}

func (waitWriter) Write(ctx context.Context, _ int64, _ time.Time, _ otlp.Batch) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestWriteTimeout(t *testing.T) {
	h := newHarness(enabledApp())
	h.h.writer = waitWriter{}
	h.h.writeTimeout = 50 * time.Millisecond
	start := time.Now()
	st, _, hdr := h.postGZ(t, validJSON())
	elapsed := time.Since(start)
	if st != 503 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "30" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("elapsed %s", elapsed)
	}
}

func TestKeyCacheNotReadyIs503(t *testing.T) {
	src := &memApps{}
	c := NewKeyCache(src, &frozenClock{t: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := newHarness(c)
	st, _, hdr := h.postGZ(t, validJSON())
	if st != 503 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "30" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
}

func TestKeyCacheKeepsMapOnFailedReload(t *testing.T) {
	src := &memApps{apps: []AppInfo{{ID: 1, ServiceName: "a", Enabled: true, IngestKey: "abc"}}}
	c := NewKeyCache(src, &frozenClock{t: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := c.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	src.err = errors.New("down")
	if err := c.Reload(context.Background()); err == nil {
		t.Fatal("expected reload error")
	}
	got, res := c.Lookup("abc")
	if res != LookupFound || got.ID != 1 {
		t.Fatalf("res=%v got=%+v", res, got)
	}
}

func TestLookupNeverCallsSource(t *testing.T) {
	src := &memApps{apps: []AppInfo{{ID: 1, ServiceName: "a", Enabled: true, IngestKey: "abc"}}}
	c := NewKeyCache(src, &frozenClock{t: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := c.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := src.calls
	_, _ = c.Lookup("abc")
	_, _ = c.Lookup("missing")
	if src.calls != n {
		t.Fatalf("lookups called source %d times (want %d)", src.calls-n, 0)
	}
}

func TestInvalidateReloads(t *testing.T) {
	src := &memApps{apps: []AppInfo{{ID: 1, ServiceName: "a", Enabled: true, IngestKey: "abc"}}}
	c := NewKeyCache(src, &frozenClock{t: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := c.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := src.calls
	src.apps = []AppInfo{{ID: 2, ServiceName: "b", Enabled: true, IngestKey: "abc"}}
	if err := c.Invalidate(); err != nil {
		t.Fatal(err)
	}
	if src.calls != n+1 {
		t.Fatalf("calls=%d want %d", src.calls, n+1)
	}
	got, res := c.Lookup("abc")
	if res != LookupFound || got.ID != 2 {
		t.Fatalf("res=%v got=%+v", res, got)
	}
}

func TestStreamedLogsBodyCapped(t *testing.T) {
	clk := &frozenClock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	w := &memWriter{}
	h := NewHandler(enabledApp(), NewLimiter(clk), w, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fa := fiber.New(app.StreamingFiberConfig("kun-telemetry-test"))
	fa.Post("/v1/logs", h.Logs)
	ready := make(chan struct{})
	go func() {
		_ = fa.Listener(ln, fiber.ListenConfig{
			DisableStartupMessage: true,
			BeforeServeFunc: func(*fiber.App) error {
				close(ready)
				return nil
			},
		})
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("fiber not ready")
	}
	t.Cleanup(func() { _ = fa.Shutdown() })
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 30 * time.Second}

	post := func(body io.Reader, cl int64, enc string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, base+"/v1/logs", body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-telemetry-key", testKey)
		if enc != "" {
			req.Header.Set("Content-Encoding", enc)
		}
		if cl < 0 {
			req.ContentLength = -1
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)
		return resp.StatusCode
	}

	chunked := io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 10<<20)))
	if st := post(chunked, -1, ""); st != 413 {
		t.Fatalf("chunked 10MiB status=%d", st)
	}

	three := bytes.Repeat([]byte("y"), 3<<20)
	if st := post(bytes.NewReader(three), int64(len(three)), ""); st != 413 {
		t.Fatalf("3MiB cl status=%d", st)
	}

	gz := gzipJSONBytes(validJSON())
	if st := post(bytes.NewReader(gz), int64(len(gz)), "gzip"); st != 200 {
		t.Fatalf("gzip batch status=%d", st)
	}
	if w.calls != 1 {
		t.Fatalf("writer calls=%d", w.calls)
	}
}
