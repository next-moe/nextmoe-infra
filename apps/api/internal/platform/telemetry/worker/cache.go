package worker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const DefaultCacheCap int64 = 4 << 30

type SymbolCache struct {
	Dir string
	Cap int64

	mu sync.Mutex
}

func (c *SymbolCache) cap() int64 {
	if c.Cap <= 0 {
		return DefaultCacheCap
	}
	return c.Cap
}

func (c *SymbolCache) Get(ctx context.Context, sha256 string, open func(context.Context) (io.ReadCloser, error)) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(c.Dir, sha256)
	if st, err := os.Stat(dest); err == nil && st.Mode().IsRegular() {
		now := time.Now()
		_ = os.Chtimes(dest, now, now)
		return dest, nil
	}
	rc, err := open(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(c.Dir, ".blob-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	_, copyErr := io.Copy(tmp, rc)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if copyErr != nil {
			return "", copyErr
		}
		if syncErr != nil {
			return "", syncErr
		}
		return "", closeErr
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	now := time.Now()
	_ = os.Chtimes(dest, now, now)
	if err := c.pruneLocked(); err != nil {
		return dest, err
	}
	return dest, nil
}

type cacheFile struct {
	path string
	size int64
	mod  time.Time
}

func (c *SymbolCache) pruneLocked() error {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return err
	}
	var files []cacheFile
	var total int64
	for _, e := range entries {
		if e.IsDir() || e.Name() == "" || e.Name()[0] == '.' {
			continue
		}
		p := filepath.Join(c.Dir, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !st.Mode().IsRegular() {
			continue
		}
		files = append(files, cacheFile{path: p, size: st.Size(), mod: st.ModTime()})
		total += st.Size()
	}
	limit := c.cap()
	if total <= limit {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= limit {
			break
		}
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		total -= f.size
	}
	return nil
}
