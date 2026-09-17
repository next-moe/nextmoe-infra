package vndbcovers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"api/pkg/imageclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadPrefersTheLocalMirror(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cv", "06"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cv", "06", "12306.jpg"), []byte("mirror-bytes"), 0o644))

	r := &runner{imageDir: dir, stats: &Stats{}}
	body, name, err := r.download(context.Background(), "https://127.0.0.1:1/cv/06/12306.jpg")
	require.NoError(t, err, "an unroutable host must never be dialed on a mirror hit")
	assert.Equal(t, "mirror-bytes", string(body))
	assert.Equal(t, "12306.jpg", name)
	assert.Equal(t, 1, r.stats.Local)
}

func TestDownloadFallsBackToHTTPWhenTheMirrorMisses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/cv/06/99999.jpg" {
			_, _ = w.Write([]byte("cdn-bytes"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	r := &runner{imageDir: t.TempDir(), stats: &Stats{}}
	body, name, err := r.download(context.Background(), srv.URL+"/cv/06/99999.jpg")
	require.NoError(t, err)
	assert.Equal(t, "cdn-bytes", string(body))
	assert.Equal(t, "99999.jpg", name)
	assert.Equal(t, 0, r.stats.Local)
}

func TestPaceIsOneSharedGapAcrossWorkers(t *testing.T) {
	r := &runner{gap: 20 * time.Millisecond}
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				assert.True(t, r.pace(context.Background()))
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 8*20*time.Millisecond,
		"9 paced slots across 3 workers must serialize to >= 8 gaps")
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

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cv", "06"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cv", "06", "12306.jpg"), []byte("mirror-bytes"), 0o644))

	r := &runner{
		cli:      imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "t", ClientSecret: "t"}),
		imageDir: dir,
		stats:    &Stats{},
	}
	r.fill(context.Background(), planRow{
		WorkID: 1,
		VNDBID: "v1",
		Img:    &vnImage{URL: "https://t.vndb.org/cv/06/12306.jpg", Dims: []int{12, 16}},
	})
	assert.Equal(t, 1, r.stats.Rejected)
	assert.Equal(t, 0, r.stats.Errors)
	assert.Equal(t, 1, uploads)
}
