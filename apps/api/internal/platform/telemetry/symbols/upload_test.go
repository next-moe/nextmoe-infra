package symbols

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"api/internal/app"
	"api/internal/platform/telemetry/ingest"

	"github.com/gofiber/fiber/v3"
)

const testTok = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeTokens struct {
	app *UploadApp
	err error
}

func (f *fakeTokens) LookupBySymbolsTokenHash(_ context.Context, hash string) (*UploadApp, error) {
	if f.err != nil {
		return nil, f.err
	}
	if hash != HashToken(testTok) {
		return nil, nil
	}
	if f.app == nil {
		return nil, nil
	}
	cp := *f.app
	return &cp, nil
}

type fakeRepo struct {
	knownID int64
	known   bool
	id      int64
	err     error
	ingests int
	last    []IncomingFile
}

func (f *fakeRepo) IngestSymbolUpload(_ context.Context, _ int64, _ string, _ string, files []IncomingFile) (int64, bool, error) {
	f.ingests++
	f.last = append([]IncomingFile(nil), files...)
	if f.err != nil {
		return 0, false, f.err
	}
	if f.known {
		return f.knownID, false, nil
	}
	if f.id == 0 {
		f.id = 7
	}
	return f.id, true, nil
}

func enabledTok() *fakeTokens {
	return &fakeTokens{app: &UploadApp{ID: 3, ServiceName: "kungal-app", Enabled: true}}
}

func validARM64() []byte {
	id := bytes.Repeat([]byte{0xab}, 16)
	return makeELF(elf.ELFCLASS64, elf.EM_AARCH64, noteGNU(id))
}

func validMap() []byte { return []byte(`["a","b"]`) }

func validR8() []byte { return []byte("com.Foo -> a.b:\n") }

type uploadServer struct {
	h    *Handler
	base string
	repo *fakeRepo
}

func startUpload(t *testing.T, tok *fakeTokens, repo *fakeRepo) *uploadServer {
	t.Helper()
	if repo == nil {
		repo = &fakeRepo{}
	}
	h := NewHandler(tok, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fa := fiber.New(app.StreamingFiberConfig("kun-telemetry-test"))
	fa.Post("/v1/symbols", h.Symbols)
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
	return &uploadServer{h: h, base: "http://" + ln.Addr().String(), repo: repo}
}

func (s *uploadServer) post(t *testing.T, token, ctype string, body io.Reader, contentLen int64) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.base+"/v1/symbols", body)
	if err != nil {
		t.Fatal(err)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentLen >= 0 {
		req.ContentLength = contentLen
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw), resp.Header
}

func multipartBody(t *testing.T, fields map[string]string, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		fw, err := w.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func stdFields() map[string]string {
	return map[string]string{"service.name": "kungal-app", "service.version": "1.0.0"}
}

func TestUploadRow1AuthFormat(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ := s.post(t, "", ctype, buf, int64(buf.Len()))
	if st != 401 {
		t.Fatalf("missing auth status=%d", st)
	}
	buf, ctype = multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ = s.post(t, "short", ctype, buf, int64(buf.Len()))
	if st != 401 {
		t.Fatalf("short token status=%d", st)
	}
	upper := strings.ToUpper(testTok)
	buf, ctype = multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ = s.post(t, upper, ctype, buf, int64(buf.Len()))
	if st != 401 {
		t.Fatalf("uppercase token status=%d", st)
	}
}

func TestUploadRow2UnknownToken(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	other := strings.Repeat("b", 64)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ := s.post(t, other, ctype, buf, int64(buf.Len()))
	if st != 401 {
		t.Fatalf("status=%d", st)
	}
}

func TestUploadRow3DisabledApp(t *testing.T) {
	tok := &fakeTokens{app: &UploadApp{ID: 3, ServiceName: "kungal-app", Enabled: false}}
	s := startUpload(t, tok, nil)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 403 {
		t.Fatalf("status=%d", st)
	}
}

func TestUploadRow4ContentType(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	st, _, _ := s.post(t, testTok, "application/json", strings.NewReader(`{}`), 2)
	if st != 415 {
		t.Fatalf("status=%d", st)
	}
	st, _, _ = s.post(t, testTok, "multipart/form-data", strings.NewReader("x"), 1)
	if st != 415 {
		t.Fatalf("no boundary status=%d", st)
	}
}

func TestUploadRow5NoSlot(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	s.h.slotWait = 40 * time.Millisecond
	s.h.sem <- struct{}{}
	s.h.sem <- struct{}{}
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, hdr := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 503 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "60" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
}

func TestUploadRow6MalformedMultipart(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	body := strings.NewReader("this is not multipart")
	st, _, _ := s.post(t, testTok, "multipart/form-data; boundary=foo", body, int64(body.Len()))
	if st != 400 {
		t.Fatalf("status=%d", st)
	}
}

func TestUploadRow7TooLarge(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	s.h.maxFile = 64
	elf := validARM64()
	if len(elf) < 65 {
		elf = append(elf, bytes.Repeat([]byte{0}, 80)...)
	}
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: elf})
	st, _, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 413 {
		t.Fatalf("file status=%d body too small? elf=%d", st, len(elf))
	}

	s2 := startUpload(t, enabledTok(), nil)
	s2.h.maxBody = 80
	buf, ctype = multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64(), FileR8Mapping: validR8()})
	st, _, _ = s2.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 413 {
		t.Fatalf("body status=%d len=%d", st, buf.Len())
	}
}

func TestUploadRow8BadPart(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	fields := stdFields()
	fields["nope"] = "x"
	buf, ctype := multipartBody(t, fields, map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("extra part status=%d", st)
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("service.name", "kungal-app")
	_ = w.WriteField("service.version", "1.0.0")
	_ = w.WriteField("service.version", "1.0.1")
	fw, _ := w.CreateFormFile("file", FileARM64Symbols)
	_, _ = fw.Write(validARM64())
	_ = w.Close()
	st, _, _ = s.post(t, testTok, w.FormDataContentType(), &b, int64(b.Len()))
	if st != 400 {
		t.Fatalf("repeated field status=%d", st)
	}

	fields = stdFields()
	fields["service.version"] = strings.Repeat("1", 257)
	buf, ctype = multipartBody(t, fields, map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ = s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("oversize field status=%d", st)
	}

	buf, ctype = multipartBody(t, stdFields(), map[string][]byte{"weird.bin": []byte("x")})
	st, _, _ = s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("unknown filename status=%d", st)
	}

	var b2 bytes.Buffer
	w2 := multipart.NewWriter(&b2)
	_ = w2.WriteField("service.name", "kungal-app")
	_ = w2.WriteField("service.version", "1.0.0")
	fw, _ = w2.CreateFormFile("file", FileARM64Symbols)
	_, _ = fw.Write(validARM64())
	fw, _ = w2.CreateFormFile("file", FileARM64Symbols)
	_, _ = fw.Write(validARM64())
	_ = w2.Close()
	st, _, _ = s.post(t, testTok, w2.FormDataContentType(), &b2, int64(b2.Len()))
	if st != 400 {
		t.Fatalf("duplicate filename status=%d", st)
	}
}

func TestUploadRow9FieldValidation(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	fields := map[string]string{"service.name": "other-app", "service.version": "1.0.0"}
	buf, ctype := multipartBody(t, fields, map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("name mismatch status=%d", st)
	}

	fields = map[string]string{"service.name": "kungal-app", "service.version": "-bad"}
	buf, ctype = multipartBody(t, fields, map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ = s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("bad version status=%d", st)
	}

	fields = stdFields()
	fields["flutter.engine_revision"] = "xyz"
	buf, ctype = multipartBody(t, fields, map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, _ = s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("bad engine status=%d", st)
	}

	buf, ctype = multipartBody(t, stdFields(), nil)
	st, _, _ = s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("no files status=%d", st)
	}
}

func TestUploadRow10FileValidation(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: []byte("not-elf")})
	st, body, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("status=%d", st)
	}
	if !strings.Contains(body, FileARM64Symbols) {
		t.Fatalf("message=%s", body)
	}
}

func TestUploadRow11StorageError(t *testing.T) {
	repo := &fakeRepo{err: errors.New("db down")}
	s := startUpload(t, enabledTok(), repo)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, _, hdr := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 503 {
		t.Fatalf("status=%d", st)
	}
	if hdr.Get("Retry-After") != "60" {
		t.Fatalf("Retry-After=%q", hdr.Get("Retry-After"))
	}
}

func TestUploadRow12Success(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{
		FileARM64Symbols: validARM64(),
		FileObfuscation:  validMap(),
		FileR8Mapping:    validR8(),
	})
	st, body, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 200 {
		t.Fatalf("status=%d body=%s", st, body)
	}
	var out uploadResult
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Created || out.UploadID != 7 || len(out.Files) != 3 {
		t.Fatalf("%+v", out)
	}
}

func TestUploadAuthBeforeBody(t *testing.T) {
	s := startUpload(t, enabledTok(), nil)
	body := bytes.Repeat([]byte("x"), 20<<20)
	st, _, _ := s.post(t, strings.Repeat("c", 64), "application/octet-stream", bytes.NewReader(body), int64(len(body)))
	if st != 401 {
		t.Fatalf("status=%d", st)
	}
}

func TestUploadStreamsLargeBody(t *testing.T) {
	repo := &fakeRepo{}
	tok := enabledTok()
	h := NewHandler(tok, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var buffered atomic.Int32
	ingest.OnBufferedBody = func() { buffered.Add(1) }
	t.Cleanup(func() { ingest.OnBufferedBody = nil })
	var stillStream atomic.Bool
	stillStream.Store(true)
	fa := fiber.New(app.StreamingFiberConfig("kun-telemetry-test"))
	fa.Post("/v1/symbols", func(c fiber.Ctx) error {
		if !c.Request().IsBodyStream() {
			stillStream.Store(false)
			t.Error("expected BodyStream")
		}
		err := h.Symbols(c)
		if !c.Request().IsBodyStream() {
			stillStream.Store(false)
			t.Error("Request().Body() closed the stream")
		}
		return err
	})
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

	elf := append(validARM64(), bytes.Repeat([]byte{0}, 40<<20)...)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: elf})
	req, err := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+"/v1/symbols", buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+testTok)
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	var out uploadResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 1 || out.Files[0].BuildID != strings.Repeat("ab", 16) {
		t.Fatalf("files=%+v", out.Files)
	}
	if buffered.Load() != 0 {
		t.Fatal("Body() fallback used")
	}
	if !stillStream.Load() {
		t.Fatal("stream was consumed via Body()")
	}
}

func TestNothingStoredOnInvalid(t *testing.T) {
	repo := &fakeRepo{}
	s := startUpload(t, enabledTok(), repo)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{
		FileARM64Symbols: validARM64(),
		FileR8Mapping:    []byte("not a mapping"),
	})
	st, _, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 400 {
		t.Fatalf("status=%d", st)
	}
	if repo.ingests != 0 {
		t.Fatalf("ingests=%d", repo.ingests)
	}
}

func TestAlreadyKnown(t *testing.T) {
	repo := &fakeRepo{known: true, knownID: 99}
	s := startUpload(t, enabledTok(), repo)
	buf, ctype := multipartBody(t, stdFields(), map[string][]byte{FileARM64Symbols: validARM64()})
	st, body, _ := s.post(t, testTok, ctype, buf, int64(buf.Len()))
	if st != 200 {
		t.Fatalf("status=%d body=%s", st, body)
	}
	var out uploadResult
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Created || out.UploadID != 99 {
		t.Fatalf("%+v", out)
	}
}
