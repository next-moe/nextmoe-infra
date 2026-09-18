package dlsitemedia

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCDNMissingCover(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "RJ3"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "RJ3", "RJ3_img_main.jpg"), []byte("x"), 0o644))

	r := &runner{
		exist: &existing{cover: map[int64]bool{}},
		cdnMissing: map[string]bool{
			"RJ1/RJ1_img_main.jpg": true,
			"RJ9/RJ2_img_main.jpg": true,
		},
	}
	r.cdnMissing["RJ3/RJ3_img_main.jpg"] = true
	ctx := context.Background()

	r.writeCover(ctx, dir, candidate{WorkID: 1, Workno: "RJ1"}, dlsiteMeta{CoverFile: "RJ1_img_main.jpg"}, false)
	r.writeCover(ctx, dir, candidate{WorkID: 2, Workno: "RJ2"}, dlsiteMeta{CoverFile: "RJ2_img_main.jpg"}, false)
	r.writeCover(ctx, dir, candidate{WorkID: 3, Workno: "RJ3"}, dlsiteMeta{CoverFile: "RJ3_img_main.jpg"}, false)

	assert.Equal(t, 1, r.c.coverCDNMissing)
	assert.Equal(t, 1, r.c.coverMissing)
	assert.Equal(t, 1, r.c.coverWould)
	assert.Equal(t, 0, r.c.coverExists)
	assert.Equal(t, map[string]bool{"RJ2": true}, r.unmirrored)
}

func TestCDNMissingScreenshots(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "RJ5"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "RJ5", "RJ5_img_smp1.jpg"), []byte("x"), 0o644))

	r := &runner{
		exist: &existing{shot: map[int64]map[int]bool{}},
		cdnMissing: map[string]bool{
			"RJ5/RJ5_img_smp2.jpg": true,
			"RJ6/RJ6_img_smp1.jpg": true,
		},
	}
	ctx := context.Background()

	r.writeScreenshots(ctx, dir, candidate{WorkID: 5, Workno: "RJ5"}, dlsiteMeta{
		SampleFiles: []string{"RJ5_img_smp1.jpg", "RJ5_img_smp2.jpg", "RJ5_img_smp3.jpg"},
	}, false)
	assert.Equal(t, 1, r.c.shotWould)
	assert.Equal(t, 1, r.c.shotCDNMissing)
	assert.Equal(t, 1, r.c.shotMissing)
	assert.Equal(t, map[string]bool{"RJ5": true}, r.unmirrored)

	r.writeScreenshots(ctx, dir, candidate{WorkID: 6, Workno: "RJ6"}, dlsiteMeta{
		SampleFiles: []string{"RJ6_img_smp1.jpg"},
	}, false)
	assert.Equal(t, 1, r.c.shotWould)
	assert.Equal(t, 2, r.c.shotCDNMissing)
	assert.Equal(t, 1, r.c.shotMissing)
	assert.Equal(t, map[string]bool{"RJ5": true}, r.unmirrored)
}

func TestLoadCDNMissing(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok")
	require.NoError(t, os.WriteFile(ok, []byte("  RJ1/a.jpg  \n\nRJ2/b.png\n"), 0o644))
	got, err := loadCDNMissing(ok)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"RJ1/a.jpg": true, "RJ2/b.png": true}, got)

	_, err = loadCDNMissing(filepath.Join(dir, "missing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read --cdn-missing")

	for _, line := range []string{"RJ1", "RJ1/a/b", "/a.jpg", "RJ1/"} {
		p := filepath.Join(t.TempDir(), "bad")
		require.NoError(t, os.WriteFile(p, []byte("RJ7/ok.jpg\n\n"+line+"\n"), 0o644))
		_, err := loadCDNMissing(p)
		require.Error(t, err, line)
		assert.Contains(t, err.Error(), "line 3:", line)
	}
}

func TestSummaryIncludesCDNMissing(t *testing.T) {
	r := &runner{c: counters{coverCDNMissing: 3, shotCDNMissing: 5}}
	sum := r.summary(Opts{Kinds: Kinds{Cover: true, Screenshot: true}}, 0)
	cover, ok := sum["cover"].(map[string]any)
	require.True(t, ok)
	shot, ok := sum["screenshot"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, cover["cdn_missing"])
	assert.Equal(t, 5, shot["cdn_missing"])
}

func TestRunChecksCDNMissingBeforeTheDatabases(t *testing.T) {
	unreachable := "host=192.0.2.1 port=5432 connect_timeout=1"
	base := Opts{DSN: unreachable, DlsiteDSN: unreachable, MirrorDir: t.TempDir()}

	intro := base
	intro.Kinds = Kinds{Intro: true}
	intro.CDNMissing = filepath.Join(t.TempDir(), "absent")
	_, err := Run(context.Background(), &config.Config{}, intro)
	require.Error(t, err)
	assert.Equal(t, "--cdn-missing applies to cover/screenshot", err.Error())

	cover := base
	cover.Kinds = Kinds{Cover: true, Screenshot: true}
	cover.CDNMissing = filepath.Join(t.TempDir(), "absent")
	_, err = Run(context.Background(), &config.Config{}, cover)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read --cdn-missing")
}
