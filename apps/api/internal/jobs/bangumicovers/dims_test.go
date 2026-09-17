package bangumicovers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPortraitFilter(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want bool
	}{
		{"tall portrait", 800, 1200, true},
		{"one-pixel taller", 1000, 1001, true},
		{"landscape", 1200, 800, false},
		{"square is not portrait", 1000, 1000, false},
		{"zero height", 800, 0, false},
		{"zero width", 0, 1200, false},
		{"negative", -1, -2, false},
	}
	for _, c := range cases {
		if got := (dimsEntry{W: c.w, H: c.h}).portrait(); got != c.want {
			t.Errorf("%s: portrait(w=%d,h=%d) = %v, want %v", c.name, c.w, c.h, got, c.want)
		}
	}
}

func TestLoadDims(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"subject_id":100,"w":800,"h":1200,"file":"100/cover.jpg"}

{"subject_id":200,"w":1200,"h":800,"file":"200/cover.jpg"}
{"subject_id":300,"w":1000,"h":1000,"file":"300/cover.jpg"}
`
	if err := os.WriteFile(filepath.Join(dir, dimsFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := loadDims(dir)
	if err != nil {
		t.Fatalf("loadDims: %v", err)
	}
	if len(d.entry) != 3 {
		t.Fatalf("want 3 entries, got %d", len(d.entry))
	}
	if e, ok := d.entry["100"]; !ok || !e.portrait() {
		t.Errorf("subject 100 must be a portrait entry, got %+v ok=%v", e, ok)
	}
	if e, ok := d.entry["200"]; !ok || e.portrait() {
		t.Errorf("subject 200 must be a non-portrait entry, got %+v ok=%v", e, ok)
	}
	if e, ok := d.entry["300"]; !ok || e.portrait() {
		t.Errorf("subject 300 (square) must be a non-portrait entry, got %+v ok=%v", e, ok)
	}
}

func TestLoadDimsErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dimsFileName), []byte(`{"subject_id":1,"w":1,"h":2}
not json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDims(dir); err == nil {
		t.Error("a malformed manifest line must error, not be silently skipped")
	}

	if _, err := loadDims(t.TempDir()); err == nil {
		t.Error("a missing dims.jsonl must error")
	}
}

func TestCoverPath(t *testing.T) {
	root := "/m"
	if got, want := coverPath(root, "100", dimsEntry{File: "100/cover.jpg"}), filepath.Join(root, "100", "cover.jpg"); got != want {
		t.Errorf("coverPath with file = %q, want %q", got, want)
	}
	if got, want := coverPath(root, "555", dimsEntry{File: ""}), filepath.Join(root, "555", "cover.jpg"); got != want {
		t.Errorf("coverPath fallback = %q, want %q", got, want)
	}
}

func TestIsBodyless(t *testing.T) {
	empty := ""
	claimed := "galgame_wiki"
	if !isBodyless(nil) || !isBodyless(&empty) {
		t.Error("nil / '' site must be bodyless")
	}
	if isBodyless(&claimed) {
		t.Error("claimed site must NOT be bodyless")
	}
}

func TestSubjectsOutListsWhatTheMirrorLacks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "40"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "40", "cover.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &dims{entry: map[string]dimsEntry{
		"30": {SubjectID: 30, W: 3, H: 4, File: "30/cover.jpg"},
		"40": {SubjectID: 40, W: 3, H: 4, File: "40/cover.jpg"},
	}}
	r := &runner{exist: map[int64]bool{2: true}}
	cands := []candidate{
		{WorkID: 1, SubjectID: "10"},
		{WorkID: 2, SubjectID: "20"},
		{WorkID: 3, SubjectID: "30"},
		{WorkID: 4, SubjectID: "40"},
		{WorkID: 5, SubjectID: "5"},
	}
	r.process(context.Background(), Opts{BangumiMirror: root}, cands, d)

	out := filepath.Join(t.TempDir(), "subjects")
	if err := writeSubjects(out, r.unmirrored); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "5\n10\n30\n" {
		t.Errorf("subjects = %q; want the unrecorded, uncovered ones and the recorded one whose file is gone, in numeric order", got)
	}
	if r.c.coverWould != 1 {
		t.Errorf("would upload = %d, want the mirrored one", r.c.coverWould)
	}
}
