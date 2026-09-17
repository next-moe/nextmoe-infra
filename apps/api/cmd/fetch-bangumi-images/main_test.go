package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api/internal/jobs/bgmimages"
)

func idsFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ids")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func noEnv(string) string { return "" }

func TestFlagsAreValidated(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"--kind", "labels", "--ids-file", "x", "--out", "y"}, noEnv, &out, &errb); rc != 2 {
		t.Errorf("unknown kind rc = %d", rc)
	}
	if rc := run(context.Background(), []string{"--kind", "covers"}, noEnv, &out, &errb); rc != 2 {
		t.Errorf("missing paths rc = %d", rc)
	}
	if rc := run(context.Background(), []string{"--kind", "covers", "--ids-file", "/nonexistent", "--out", t.TempDir()}, noEnv, &out, &errb); rc != 1 {
		t.Errorf("unreadable ids rc = %d", rc)
	}
}

func TestAnEmptyListIsAQuietSuccess(t *testing.T) {
	var out, errb bytes.Buffer
	rc := run(context.Background(), []string{"--kind", "persons", "--ids-file", idsFile(t, "\n# none\n"), "--out", t.TempDir()}, noEnv, &out, &errb)
	if rc != 0 {
		t.Fatalf("rc = %d, stderr %s", rc, errb.String())
	}
	want := "fetch-bangumi-images: done — kind=persons ids=0 downloaded=0 skipped_exist=0 no_image=0 not_found=0 errors=0\n"
	if out.String() != want {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestPaceDefaultsPerKindUnlessSet(t *testing.T) {
	if r, c := pace(bgmimages.KindCovers, map[string]bool{}, 0, 0); r != 2 || c != 3 {
		t.Errorf("covers defaults = %v %v", r, c)
	}
	if r, c := pace(bgmimages.KindPersons, map[string]bool{}, 0, 0); r != 1 || c != 2 {
		t.Errorf("persons defaults = %v %v", r, c)
	}
	if r, c := pace(bgmimages.KindPersons, map[string]bool{"rate": true, "concurrency": true}, 0.5, 7); r != 0.5 || c != 7 {
		t.Errorf("explicit flags = %v %v", r, c)
	}
}

func TestTheTokenComesFromTheEnvironment(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "nextmoe-infra/fetch-bangumi-images") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	env := func(k string) string {
		if k == "KUN_BANGUMI_TOKEN" {
			return "from-env"
		}
		return ""
	}
	var out, errb bytes.Buffer
	rc := run(context.Background(), []string{"--kind", "covers", "--ids-file", idsFile(t, "42\n"), "--out", t.TempDir(), "--api-base", srv.URL}, env, &out, &errb)
	if rc != 0 {
		t.Fatalf("rc = %d, stderr %s", rc, errb.String())
	}
	if auth != "Bearer from-env" {
		t.Errorf("Authorization = %q", auth)
	}
	if !strings.Contains(out.String(), "ids=1 downloaded=0 skipped_exist=0 no_image=0 not_found=1 errors=0") {
		t.Errorf("stdout = %q", out.String())
	}
}
