package dlsitemedia

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"api/pkg/imageclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadAllOverlapsUploadsAndKeepsEachResultOnItsJob(t *testing.T) {
	var inFlight, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		require.NoError(t, r.ParseMultipartForm(1<<20))
		f, _, err := r.FormFile("file")
		require.NoError(t, err)
		body, _ := io.ReadAll(f)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"hash": string(body)}})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	var jobs []uploadJob
	for i, name := range []string{"a", "b", "c", "d", "e", "f"} {
		p := filepath.Join(dir, name+".jpg")
		require.NoError(t, os.WriteFile(p, []byte(name), 0o644))
		jobs = append(jobs, uploadJob{sort: i, path: p, filename: name + ".jpg"})
	}
	r := &runner{workers: 3, cli: imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "t", ClientSecret: "t"})}
	r.uploadAll(context.Background(), jobs, shotPreset)

	assert.EqualValues(t, 3, peak.Load(), "three workers keep three uploads in flight")
	for i, j := range jobs {
		require.NoError(t, j.err)
		assert.Equal(t, string(rune('a'+i)), j.res.Hash, "job %d", i)
	}
}

func TestPaceIsOneSharedGapAcrossWorkers(t *testing.T) {
	r := &runner{gap: 20 * time.Millisecond}
	start := time.Now()
	done := make(chan struct{})
	for range 4 {
		go func() { r.pace(); done <- struct{}{} }()
	}
	for range 4 {
		<-done
	}
	assert.GreaterOrEqual(t, time.Since(start), 60*time.Millisecond, "four slots one gap apart")
}
