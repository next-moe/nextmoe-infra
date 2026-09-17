package charportraits

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForecastMirrorListsEachMissingFileOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ch", "01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ch", "01", "101.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := "abc"
	f := forecastMirror([]candidate{
		{CatalogCharacterID: 1, ImageID: "ch7", ImageHash: &hash},
		{CatalogCharacterID: 2, ImageID: "ch101"},
		{CatalogCharacterID: 3, ImageID: "ch250"},
		{CatalogCharacterID: 4, ImageID: "ch250"},
		{CatalogCharacterID: 5, ImageID: "ch12"},
		{CatalogCharacterID: 6, ImageID: "cvx"},
	}, dir)

	if f.hasHash != 1 || f.present != 1 || f.badID != 1 {
		t.Fatalf("forecast = %+v", f)
	}
	if got, want := f.missingList(), "ch/12/12.jpg\nch/50/250.jpg\n"; got != want {
		t.Errorf("missing list = %q, want %q", got, want)
	}
}
