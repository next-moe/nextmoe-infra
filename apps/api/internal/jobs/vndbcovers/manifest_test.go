package vndbcovers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "manifest.csv")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func TestLoadManifest(t *testing.T) {
	p := writeManifest(t, `vndb_id,url,width,height,sexual,violence
v17,https://t.vndb.org/cv/17/17.jpg,560,650,1.5,0
v99,,0,0,0,0
`)
	m, err := loadManifest(p)
	require.NoError(t, err)
	require.Len(t, m, 2)

	img := m["v17"]
	require.NotNil(t, img)
	assert.Equal(t, "https://t.vndb.org/cv/17/17.jpg", img.URL)
	assert.Equal(t, []int{560, 650}, img.Dims)
	assert.Equal(t, 1.5, img.Sexual)
	assert.Equal(t, float64(0), img.Violence)

	got, known := m["v99"]
	assert.True(t, known, "an imageless VN must stay in the map so it reads as no-image, not vn-unknown")
	assert.Nil(t, got)
}

func TestLoadManifestRejectsAForeignHeader(t *testing.T) {
	p := writeManifest(t, "id,link\nv1,x\n")
	_, err := loadManifest(p)
	assert.Error(t, err)
}

func TestLoadManifestRejectsGarbageNumbers(t *testing.T) {
	p := writeManifest(t, `vndb_id,url,width,height,sexual,violence
v17,https://t.vndb.org/cv/17/17.jpg,wide,650,0,0
`)
	_, err := loadManifest(p)
	assert.Error(t, err)
}

func TestLoadManifestMissingFile(t *testing.T) {
	_, err := loadManifest(filepath.Join(t.TempDir(), "absent.csv"))
	assert.Error(t, err)
}
