package symbols

import (
	"archive/zip"
	"bytes"
	"context"
	"debug/elf"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"api/internal/platform/telemetry/model"
)

type memBlobs struct {
	m map[string][]byte
}

func (m *memBlobs) Put(_ context.Context, key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if m.m == nil {
		m.m = map[string][]byte{}
	}
	m.m[key] = b
	return nil
}

func (m *memBlobs) Open(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.m[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memBlobs) Delete(_ context.Context, key string) error {
	if _, ok := m.m[key]; !ok {
		return fs.ErrNotExist
	}
	delete(m.m, key)
	return nil
}

type fakeEng struct {
	row *model.EngineSymbol
}

func (f *fakeEng) NextPendingEngine(_ context.Context, now time.Time) (*model.EngineSymbol, error) {
	if f.row == nil || f.row.Status != StatusPending || f.row.NextAttemptAt.After(now) {
		return nil, nil
	}
	cp := *f.row
	return &cp, nil
}

func (f *fakeEng) SaveEngineSymbol(_ context.Context, row *model.EngineSymbol) error {
	cp := *row
	f.row = &cp
	return nil
}

func (f *fakeEng) EnsureBlobRow(context.Context, string, int64) error { return nil }

type frozenNow struct{ t time.Time }

func (c frozenNow) Now() time.Time { return c.t }

func zipBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		fw, err := w.Create(name)
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
	return buf.Bytes()
}

func arm64SO() []byte {
	id := bytes.Repeat([]byte{0x11}, 16)
	return makeELF(elf.ELFCLASS64, elf.EM_AARCH64, noteGNU(id))
}

func armSO() []byte {
	id := bytes.Repeat([]byte{0x22}, 16)
	return makeELF(elf.ELFCLASS32, elf.EM_ARM, noteGNU(id))
}

func startFetch(t *testing.T, handler http.Handler, row *model.EngineSymbol) (*Fetcher, *fakeEng, *memBlobs) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	eng := &fakeEng{row: row}
	blobs := &memBlobs{}
	f := NewFetcher(eng, blobs, srv.URL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.client = srv.Client()
	f.clock = frozenNow{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	return f, eng, blobs
}

func pendingRow(variant string) *model.EngineSymbol {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	return &model.EngineSymbol{
		EngineRevision: "0123456789abcdef0123456789abcdef01234567",
		Variant:        variant,
		Status:         StatusPending,
		NextAttemptAt:  now,
	}
}

func TestEngineFetchReady(t *testing.T) {
	row := pendingRow(VariantARM64Release)
	so := arm64SO()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+row.EngineRevision+"/"+row.Variant+"/symbols.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes(t, map[string][]byte{"libflutter.so": so, "LICENSE": []byte("x")}))
	})
	f, eng, blobs := startFetch(t, mux, row)
	if err := f.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if eng.row.Status != StatusReady {
		t.Fatalf("status=%s err=%s", eng.row.Status, eng.row.LastError)
	}
	if eng.row.BuildID != "11111111111111111111111111111111" {
		t.Fatalf("build-id=%s", eng.row.BuildID)
	}
	if len(blobs.m) != 1 {
		t.Fatalf("blobs=%d", len(blobs.m))
	}
}

func TestEngineFetch404(t *testing.T) {
	row := pendingRow(VariantARM64Release)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	f, eng, _ := startFetch(t, mux, row)
	if err := f.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if eng.row.Status != StatusFailed || eng.row.LastError != "not found" {
		t.Fatalf("status=%s err=%s", eng.row.Status, eng.row.LastError)
	}
}

func TestEngineFetchRetryBackoff(t *testing.T) {
	row := pendingRow(VariantARM64Release)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	})
	f, eng, _ := startFetch(t, mux, row)
	now := f.clock.Now()
	if err := f.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if eng.row.Status != StatusPending || eng.row.Attempts != 1 {
		t.Fatalf("status=%s attempts=%d", eng.row.Status, eng.row.Attempts)
	}
	want := now.Add(2 * time.Minute)
	if !eng.row.NextAttemptAt.Equal(want) {
		t.Fatalf("next=%s want %s", eng.row.NextAttemptAt, want)
	}
}

func TestEngineFetchGivesUpAfterTen(t *testing.T) {
	row := pendingRow(VariantARM64Release)
	row.Attempts = 9
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	})
	f, eng, _ := startFetch(t, mux, row)
	if err := f.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if eng.row.Status != StatusFailed || eng.row.Attempts != 10 {
		t.Fatalf("status=%s attempts=%d", eng.row.Status, eng.row.Attempts)
	}
}

func TestEngineFetchMachineMismatch(t *testing.T) {
	row := pendingRow(VariantARM64Release)
	mux := http.NewServeMux()
	mux.HandleFunc("/"+row.EngineRevision+"/"+row.Variant+"/symbols.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes(t, map[string][]byte{"libflutter.so": armSO()}))
	})
	f, eng, blobs := startFetch(t, mux, row)
	if err := f.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if eng.row.Status != StatusFailed || eng.row.LastError != "machine mismatch" {
		t.Fatalf("status=%s err=%s", eng.row.Status, eng.row.LastError)
	}
	if len(blobs.m) != 0 {
		t.Fatal("stored a blob")
	}
}

func TestEngineFetchNoLibflutter(t *testing.T) {
	row := pendingRow(VariantARM64Release)
	mux := http.NewServeMux()
	mux.HandleFunc("/"+row.EngineRevision+"/"+row.Variant+"/symbols.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipBytes(t, map[string][]byte{"LICENSE": []byte("x")}))
	})
	f, eng, _ := startFetch(t, mux, row)
	if err := f.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if eng.row.Status != StatusPending || eng.row.Attempts != 1 {
		t.Fatalf("status=%s attempts=%d err=%s", eng.row.Status, eng.row.Attempts, eng.row.LastError)
	}
}
