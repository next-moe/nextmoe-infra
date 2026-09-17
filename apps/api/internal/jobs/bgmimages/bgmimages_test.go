package bgmimages

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type item struct {
	status   int
	images   map[string]string
	body     []byte
	ctype    string
	fail429  int32
	serveErr bool
}

type fakeBangumi struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	items    map[string]*item
	apiHits  atomic.Int64
	cdnHits  atomic.Int64
	lastAuth atomic.Value
}

func newFake(t *testing.T) *fakeBangumi {
	f := &fakeBangumi{t: t, items: map[string]*item{}}
	mux := http.NewServeMux()
	api := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("User-Agent") == "" {
				http.Error(w, "blank UA", http.StatusBadRequest)
				return
			}
			f.apiHits.Add(1)
			f.lastAuth.Store(r.Header.Get("Authorization"))
			f.mu.Lock()
			it := f.items[kind+"/"+r.PathValue("id")]
			f.mu.Unlock()
			switch {
			case it == nil:
				http.NotFound(w, r)
			case it.serveErr:
				http.Error(w, "boom", http.StatusInternalServerError)
			case atomic.AddInt32(&it.fail429, -1) >= 0:
				http.Error(w, "slow down", http.StatusTooManyRequests)
			default:
				images := map[string]any{}
				for k, v := range it.images {
					images[k] = strings.ReplaceAll(v, "{srv}", f.srv.URL)
				}
				var payload any = map[string]any{"images": images}
				if len(it.images) == 0 {
					payload = map[string]any{"images": nil}
				}
				_ = json.NewEncoder(w).Encode(payload)
			}
		}
	}
	mux.HandleFunc("GET /v0/subjects/{id}", api("subjects"))
	mux.HandleFunc("GET /v0/persons/{id}", api("persons"))
	mux.HandleFunc("GET /pic/{name}", func(w http.ResponseWriter, r *http.Request) {
		f.cdnHits.Add(1)
		f.mu.Lock()
		it := f.items["pic/"+r.PathValue("name")]
		f.mu.Unlock()
		if it == nil {
			http.NotFound(w, r)
			return
		}
		if it.ctype != "" {
			w.Header().Set("Content-Type", it.ctype)
		}
		_, _ = w.Write(it.body)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeBangumi) add(key string, it *item) {
	f.mu.Lock()
	f.items[key] = it
	f.mu.Unlock()
}

func opts(f *fakeBangumi, kind Kind, out string) Opts {
	return Opts{Kind: kind, Out: out, APIBase: f.srv.URL, UserAgent: "test-agent",
		Rate: 1000, Concurrency: 3, MaxRetries: 2}
}

func readManifest(t *testing.T, root string) []manifestRecord {
	t.Helper()
	f, err := os.Open(filepath.Join(root, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []manifestRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r manifestRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("manifest line %q: %v", sc.Text(), err)
		}
		out = append(out, r)
	}
	return out
}

func TestCoversMirrorTheLargeImageAndRecordItsSize(t *testing.T) {
	f := newFake(t)
	f.add("subjects/1", &item{images: map[string]string{"large": "{srv}/pic/1.jpg", "medium": "{srv}/pic/x.jpg"}})
	f.add("pic/1.jpg", &item{body: encodeJPEG(t, 30, 40)})
	f.add("subjects/2", &item{})
	f.add("subjects/4", &item{images: map[string]string{"medium": "{srv}/pic/4.jpg"}})
	root := t.TempDir()

	st, err := Run(context.Background(), opts(f, KindCovers, root), []int{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if st.Downloaded != 1 || st.NoImage != 2 || st.NotFound != 1 || st.Errors != 0 {
		t.Fatalf("stats = %+v", st)
	}
	if !fileExists(filepath.Join(root, "1", "cover.jpg")) {
		t.Fatal("cover not mirrored")
	}
	m := readManifest(t, root)
	if len(m) != 1 || m[0].SubjectID != 1 || m[0].File != "1/cover.jpg" || m[0].W != 30 || m[0].H != 40 || m[0].ID != "" {
		t.Fatalf("manifest = %+v", m)
	}
	if got := st.Line(); got != "fetch-bangumi-images: done — kind=covers ids=4 downloaded=1 skipped_exist=0 no_image=2 not_found=1 errors=0" {
		t.Errorf("line = %q", got)
	}
}

func TestACoverRunResumesWithoutTheNetwork(t *testing.T) {
	f := newFake(t)
	f.add("subjects/1", &item{images: map[string]string{"large": "{srv}/pic/1.jpg"}})
	f.add("pic/1.jpg", &item{body: encodeJPEG(t, 10, 20)})
	root := t.TempDir()
	if _, err := Run(context.Background(), opts(f, KindCovers, root), []int{1}); err != nil {
		t.Fatal(err)
	}
	hits := f.apiHits.Load() + f.cdnHits.Load()

	st, err := Run(context.Background(), opts(f, KindCovers, root), []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if st.SkippedExist != 1 || st.Downloaded != 0 || f.apiHits.Load()+f.cdnHits.Load() != hits {
		t.Fatalf("rerun stats = %+v, hits %d -> %d", st, hits, f.apiHits.Load()+f.cdnHits.Load())
	}
	if n := len(readManifest(t, root)); n != 1 {
		t.Fatalf("manifest has %d lines after a rerun", n)
	}

	if err := os.WriteFile(filepath.Join(root, manifestName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), opts(f, KindCovers, root), []int{1}); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, root)
	if len(m) != 1 || m[0].File != "1/cover.jpg" || m[0].H != 20 {
		t.Fatalf("a present but unrecorded cover must be recorded offline: %+v", m)
	}
	if f.apiHits.Load()+f.cdnHits.Load() != hits {
		t.Fatal("the backfill touched the network")
	}
}

func TestPersonLogosAreNamedByTheirBytesAndRecordedFromTheRoot(t *testing.T) {
	f := newFake(t)
	f.add("persons/7", &item{images: map[string]string{"large": "{srv}/pic/7.jpg"}})
	f.add("pic/7.jpg", &item{body: encodePNG(t, 64, 32), ctype: "image/jpeg"})
	f.add("persons/8", &item{images: map[string]string{"grid": "{srv}/pic/8.jpg"}})
	f.add("pic/8.jpg", &item{body: encodeJPEG(t, 8, 8)})
	f.add("persons/9", &item{})
	root := t.TempDir()

	st, err := Run(context.Background(), opts(f, KindPersons, root), []int{7, 8, 9})
	if err != nil {
		t.Fatal(err)
	}
	if st.Downloaded != 2 || st.NoImage != 1 || st.Errors != 0 {
		t.Fatalf("stats = %+v", st)
	}
	byID := map[string]manifestRecord{}
	for _, r := range readManifest(t, root) {
		byID[r.ID] = r
	}
	if byID["7"].File != "7/logo.png" || byID["7"].W != 64 || byID["7"].URL != f.srv.URL+"/pic/7.jpg" {
		t.Errorf("person 7 = %+v", byID["7"])
	}
	if byID["8"].File != "8/logo.jpg" {
		t.Errorf("person 8 = %+v", byID["8"])
	}
	for id, r := range byID {
		if !fileExists(filepath.Join(root, r.File)) {
			t.Errorf("person %s: %s does not resolve from the mirror root", id, r.File)
		}
		if r.SubjectID != 0 {
			t.Errorf("person %s carries a subject id", id)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "7", logoStaging)); !errors.Is(err, os.ErrNotExist) {
		t.Error("staging file left behind")
	}

	st, err = Run(context.Background(), opts(f, KindPersons, root), []int{7})
	if err != nil || st.SkippedExist != 1 {
		t.Fatalf("rerun = %+v, %v", st, err)
	}
}

func TestAnUndecodableLogoIsAnErrorWithoutAManifestLine(t *testing.T) {
	f := newFake(t)
	f.add("persons/5", &item{images: map[string]string{"large": "{srv}/pic/5.webp"}})
	f.add("pic/5.webp", &item{body: []byte("not an image")})
	root := t.TempDir()

	st, err := Run(context.Background(), opts(f, KindPersons, root), []int{5})
	if err != nil {
		t.Fatal(err)
	}
	if st.Errors != 1 || st.Downloaded != 0 {
		t.Fatalf("stats = %+v", st)
	}
	if !fileExists(filepath.Join(root, "5", "logo.webp")) {
		t.Error("the bytes stay on disk so a rerun skips them")
	}
	if n := len(readManifest(t, root)); n != 0 {
		t.Errorf("manifest has %d lines", n)
	}
}

func TestServerErrorsAreCountedAndA429IsRetried(t *testing.T) {
	f := newFake(t)
	f.add("subjects/1", &item{serveErr: true})
	f.add("subjects/2", &item{images: map[string]string{"large": "{srv}/pic/2.jpg"}, fail429: 1})
	f.add("pic/2.jpg", &item{body: encodeJPEG(t, 5, 5)})
	o := opts(f, KindCovers, t.TempDir())
	o.MaxRetries = 1

	st, err := Run(context.Background(), o, []int{1, 2})
	if err != nil {
		t.Fatalf("a failing id must not fail the run: %v", err)
	}
	if st.Errors != 1 || st.Downloaded != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestTheTokenRidesTheAuthorizationHeader(t *testing.T) {
	f := newFake(t)
	f.add("subjects/1", &item{})
	o := opts(f, KindCovers, t.TempDir())
	o.Token = "secret-token"
	if _, err := Run(context.Background(), o, []int{1}); err != nil {
		t.Fatal(err)
	}
	if got := f.lastAuth.Load(); got != "Bearer secret-token" {
		t.Errorf("Authorization = %v", got)
	}
	o.Token = ""
	if _, err := Run(context.Background(), o, []int{1}); err != nil {
		t.Fatal(err)
	}
	if got := f.lastAuth.Load(); got != "" {
		t.Errorf("anonymous run sent %v", got)
	}
}

func TestThePacerSpacesConcurrentCallers(t *testing.T) {
	p := newPacer(20)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.wait(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := time.Since(start); got < 240*time.Millisecond {
		t.Errorf("6 calls at 20/s took %v, want >= 250ms minus slack", got)
	}
}

func TestCancellingStopsTheRun(t *testing.T) {
	f := newFake(t)
	ids := make([]int, 50)
	for i := range ids {
		ids[i] = i + 1
		f.add("subjects/"+strconv.Itoa(i+1), &item{})
	}
	o := opts(f, KindCovers, t.TempDir())
	o.Rate = 5
	o.Concurrency = 1
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	st, err := Run(ctx, o, ids)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second || st.NoImage >= 50 {
		t.Fatalf("run did not stop: %v, %+v", time.Since(start), st)
	}
}

func TestLoadIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ids")
	if err := os.WriteFile(path, []byte("12\n\n# note\n7\n12\nabc\n-3\n 5 \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ids, err := LoadIDs(path)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[12 7 5]" {
		t.Errorf("ids = %v", ids)
	}
}

func TestLogoExt(t *testing.T) {
	for _, c := range []struct{ format, ct, url, want string }{
		{"jpeg", "image/png", "x.png", "jpg"},
		{"webp", "", "", "webp"},
		{"", "image/png", "x.jpg", "png"},
		{"", "", "x.GIF", "gif"},
		{"", "text/html", "x", "jpg"},
	} {
		if got := logoExt(c.format, c.ct, c.url); got != c.want {
			t.Errorf("logoExt(%q,%q,%q) = %q, want %q", c.format, c.ct, c.url, got, c.want)
		}
	}
}

func TestAnAlreadyCancelledRunReportsItAndFetchesNothing(t *testing.T) {
	f := newFake(t)
	f.add("subjects/1", &item{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 20; i++ {
		_, err := Run(ctx, opts(f, KindCovers, t.TempDir()), []int{1, 2, 3})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled: a cut-short run is not a success", err)
		}
	}
	if f.apiHits.Load() != 0 {
		t.Errorf("a cancelled run reached the API %d times", f.apiHits.Load())
	}
}
