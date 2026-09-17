package labellogos

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"api/pkg/imageclient"

	"gorm.io/datatypes"
)

func TestParseSource(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wantKey  string
		wantStem string
		wantErr  bool
	}{
		{in: "bangumi", wantKey: "bangumi", wantStem: "logo"},
		{in: " CIEN ", wantKey: "cien", wantStem: "avatar"},
		{in: "", wantErr: true},
		{in: "vndb", wantErr: true},
	} {
		got, err := ParseSource(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseSource(%q): want error, got %+v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseSource(%q): %v", tc.in, err)
		}
		if got.Key != tc.wantKey || got.FileStem != tc.wantStem {
			t.Fatalf("ParseSource(%q) = %+v, want key=%s stem=%s", tc.in, got, tc.wantKey, tc.wantStem)
		}
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMirrorNoRootAndNoManifest(t *testing.T) {
	m, err := loadMirror("", SourceBangumi)
	if err != nil {
		t.Fatalf("loadMirror(\"\"): %v", err)
	}
	if m.has("123") {
		t.Fatal("empty root resolved bytes")
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "123", "logo.png"))
	m, err = loadMirror(root, SourceBangumi)
	if err != nil {
		t.Fatalf("loadMirror(no manifest): %v", err)
	}
	got, ok := m.resolve("123")
	if !ok || filepath.Base(got) != "logo.png" {
		t.Fatalf("resolve = %q, %v; want .../logo.png, true", got, ok)
	}
}

func TestMirrorResolveManifestAndStemPerSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "7", "avatar.webp"))
	writeFile(t, filepath.Join(root, "9", "shot.gif"))
	manifest := `{"id":"9","file":"9/shot.gif","w":100,"h":100,"url":"https://example.test/9.gif"}
{"id":"404","file":"404/avatar.jpg","w":1,"h":1,"url":""}

`
	if err := os.WriteFile(filepath.Join(root, dimsFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := loadMirror(root, SourceCien)
	if err != nil {
		t.Fatalf("loadMirror: %v", err)
	}
	if got, ok := m.resolve("7"); !ok || filepath.Base(got) != "avatar.webp" {
		t.Fatalf("resolve(7) = %q, %v", got, ok)
	}
	if got, ok := m.resolve("9"); !ok || filepath.Base(got) != "shot.gif" {
		t.Fatalf("resolve(9) = %q, %v", got, ok)
	}
	if _, ok := m.resolve("404"); ok {
		t.Fatal("resolve(404): manifest row without bytes must not resolve")
	}
	mb, err := loadMirror(root, SourceBangumi)
	if err != nil {
		t.Fatal(err)
	}
	if mb.has("7") {
		t.Fatal("bangumi lane resolved a cien avatar")
	}
}

func TestLoadMirrorRejectsCorruptManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, dimsFileName), []byte("{not json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMirror(root, SourceBangumi); err == nil {
		t.Fatal("corrupt manifest must be a hard error, not a silent skip")
	}
}

func TestMergeProvenancePrependsAndPreservesOtherFields(t *testing.T) {
	cur := datatypes.JSON(`{"display_name":[{"source":"vndb","at":"2020-01-01T00:00:00Z"}],"logo_hash":[{"source":"cien","at":"2021-01-01T00:00:00Z"}]}`)
	out := mergeProvenance(cur, "bangumi", "2026-08-05T00:00:00Z")

	var doc map[string][]provEntry
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal merged provenance: %v", err)
	}
	if len(doc["display_name"]) != 1 || doc["display_name"][0].Source != "vndb" {
		t.Fatalf("other fields must survive untouched: %+v", doc["display_name"])
	}
	got := doc[provField]
	if len(got) != 2 {
		t.Fatalf("logo_hash provenance = %+v, want 2 entries", got)
	}
	if got[0].Source != "bangumi" || got[0].At != "2026-08-05T00:00:00Z" {
		t.Fatalf("latest entry must be first: %+v", got[0])
	}
	if got[1].Source != "cien" {
		t.Fatalf("history must be kept: %+v", got[1])
	}
}

func TestMergeProvenanceFromEmptyAndUnreadable(t *testing.T) {
	for name, cur := range map[string]datatypes.JSON{
		"empty":      nil,
		"blank":      datatypes.JSON(`{}`),
		"unreadable": datatypes.JSON(`not json at all`),
	} {
		out := mergeProvenance(cur, "cien", "2026-08-05T00:00:00Z")
		var doc map[string][]provEntry
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if len(doc[provField]) != 1 || doc[provField][0].Source != "cien" {
			t.Fatalf("%s: want a single cien entry, got %+v", name, doc[provField])
		}
	}
}

func TestWriteIDsOnlyListsIDsWithoutBytes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "20", "logo.jpg"))
	m, err := loadMirror(root, SourceBangumi)
	if err != nil {
		t.Fatal(err)
	}
	cands := []candidate{
		{LabelID: 1, ExternalID: "30"},
		{LabelID: 2, ExternalID: "20"},
		{LabelID: 3, ExternalID: "10"},
		{LabelID: 4, ExternalID: "30"},
	}
	out := filepath.Join(t.TempDir(), "ids.txt")
	n, err := writeIDs(out, cands, m)
	if err != nil {
		t.Fatalf("writeIDs: %v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || string(body) != "10\n30\n" {
		t.Fatalf("writeIDs = %d, %q; want 2, \"10\\n30\\n\"", n, string(body))
	}
}

func TestWindow(t *testing.T) {
	c := []candidate{{LabelID: 1}, {LabelID: 2}, {LabelID: 3}}
	if got := window(c, 0, 0); len(got) != 3 {
		t.Fatalf("no window = %d", len(got))
	}
	if got := window(c, 2, 1); len(got) != 2 || got[0].LabelID != 2 {
		t.Fatalf("window(2,1) = %+v", got)
	}
	if got := window(c, 0, 9); got != nil {
		t.Fatalf("offset past the end = %+v, want nil", got)
	}
}

type fakeUploader struct {
	hash   string
	err    error
	pinged []string
}

func (f *fakeUploader) UploadWithSub(_ context.Context, _ io.Reader, _, _, _ string) (*imageclient.UploadResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &imageclient.UploadResult{Hash: f.hash}, nil
}

func (f *fakeUploader) ReferencePing(_ context.Context, hashes []string) (*imageclient.ReferencePingResult, error) {
	f.pinged = append(f.pinged, hashes...)
	return &imageclient.ReferencePingResult{Updated: int64(len(hashes))}, nil
}

func TestFillDryRunNeverUploads(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "5", "logo.jpg"))
	m, err := loadMirror(root, SourceBangumi)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeUploader{hash: "nope"}
	r := &runner{cli: fake, source: SourceBangumi, mirror: m, stats: &Stats{}}

	got := r.fill(context.Background(), candidate{LabelID: 1, ExternalID: "5"}, false)
	if got.would != 1 || got.uploaded != 0 || got.hash != "" {
		t.Fatalf("dry run = %+v, want would=1 and nothing else", got)
	}
	got = r.fill(context.Background(), candidate{LabelID: 2, ExternalID: "404"}, true)
	if got.missing != 1 || got.uploaded != 0 {
		t.Fatalf("absent bytes = %+v, want missing=1", got)
	}
	if len(fake.pinged) != 0 {
		t.Fatal("dry run pinged the image service")
	}
}

func TestUploadRetriesTransientButNotTerminal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "1", "logo.jpg")
	writeFile(t, path)

	for _, terminal := range []error{imageclient.ErrQuotaExceeded, imageclient.ErrModerationRejected} {
		counting := &countingUploader{err: terminal}
		r := &runner{cli: counting, source: SourceBangumi, stats: &Stats{}}
		if _, err := r.upload(context.Background(), path); !errors.Is(err, terminal) {
			t.Fatalf("upload err = %v, want %v", err, terminal)
		}
		if counting.calls != 1 {
			t.Fatalf("terminal error retried %d times", counting.calls-1)
		}
	}

	counting := &countingUploader{err: errors.New("connection refused")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &runner{cli: counting, source: SourceBangumi, stats: &Stats{}}
	if _, err := r.upload(ctx, path); err == nil {
		t.Fatal("transient failure must surface as an error once retries are exhausted")
	}
	if counting.calls == 0 {
		t.Fatal("transient error was never attempted")
	}
}

type countingUploader struct {
	err   error
	calls int
}

func (c *countingUploader) UploadWithSub(_ context.Context, _ io.Reader, _, _, _ string) (*imageclient.UploadResult, error) {
	c.calls++
	return nil, c.err
}

func (c *countingUploader) ReferencePing(_ context.Context, _ []string) (*imageclient.ReferencePingResult, error) {
	return &imageclient.ReferencePingResult{}, nil
}

func TestFillDecodeFailedIsRejectedWithoutRetry(t *testing.T) {
	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":80010,"message":"图片解码失败"}`))
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "5", "logo.jpg"))
	m, err := loadMirror(root, SourceBangumi)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{
		cli:    imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "t", ClientSecret: "t"}),
		source: SourceBangumi,
		mirror: m,
		stats:  &Stats{},
	}
	got := r.fill(context.Background(), candidate{LabelID: 1, ExternalID: "5"}, true)
	if got.rejected != 1 || got.errors != 0 || got.quota {
		t.Fatalf("fill = %+v, want rejected=1 errors=0 quota=false", got)
	}
	if uploads != 1 {
		t.Fatalf("uploads = %d, want 1", uploads)
	}
}
