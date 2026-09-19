package tagcanon

import (
	"path/filepath"
	"testing"
)

func TestParseTagMapMissingFile(t *testing.T) {
	if _, err := ParseTagMap(filepath.Join(t.TempDir(), "nope.ts")); err == nil {
		t.Fatal("want an error for a missing tagMap, got nil")
	}
}
