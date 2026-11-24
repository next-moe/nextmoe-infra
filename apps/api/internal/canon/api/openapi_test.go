package api

import (
	"bytes"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite docs/catalog/v3-openapi.yaml")

const specPath = "../../../../../docs/catalog/v3-openapi.yaml"

func TestOpenAPIIsCurrent(t *testing.T) {
	_, h := New(nil)
	got, err := h.OpenAPI().YAML()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(specPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("docs/catalog/v3-openapi.yaml is stale; run go test ./internal/canon/api -run TestOpenAPIIsCurrent -update")
	}
}
