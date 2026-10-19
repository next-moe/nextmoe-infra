package getchuportraits

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

type runner struct {
	db         *gorm.DB
	cli        *imageclient.Client
	slot       Slot
	gap        time.Duration
	stats      *Stats
	pingHashes []string
}

type charResult struct {
	missing, uploaded, rejected, errors int
	quota                               bool
	hash                                string
}

func (s *Stats) merge(r charResult) {
	s.Missing += r.missing
	s.Uploaded += r.uploaded
	s.Rejected += r.rejected
	s.Errors += r.errors
	if r.quota {
		s.Quota = true
	}
}

func (r *runner) fill(ctx context.Context, dir string, c candidate, apply bool) charResult {
	var out charResult
	path := mirrorPath(dir, c.GetchuID, c.File)
	if !fileExists(path) {
		out.missing++
		return out
	}
	if !apply {
		return out
	}

	res, err := r.upload(ctx, path, c.File)
	if err != nil {
		switch {
		case stderrors.Is(err, imageclient.ErrQuotaExceeded):
			slog.Warn("daily image quota exhausted — stopping", "character", c.CharacterID)
			out.quota = true
		case stderrors.Is(err, imageclient.ErrModerationRejected):
			out.rejected++
			slog.Warn("image rejected by moderation", "slot", r.slot.Name, "character", c.CharacterID, "getchu", c.GetchuID, "file", c.File)
		default:
			out.errors++
			slog.Warn("upload character image", "slot", r.slot.Name, "character", c.CharacterID, "getchu", c.GetchuID, "file", c.File, "err", err)
		}
		return out
	}

	tx := r.db.WithContext(ctx).Exec(fmt.Sprintf(
		`UPDATE catalog_character SET %[1]s = ?, updated_at = now()
		 WHERE id = ? AND %[1]s IS NULL AND deleted_at IS NULL`, r.slot.TargetColumn)+" AND "+editspec.NotHumanFieldSQL("field_provenance", r.slot.TargetColumn),
		res.Hash, c.CharacterID)
	if tx.Error != nil {
		out.errors++
		slog.Warn("write character image", "slot", r.slot.Name, "character", c.CharacterID, "err", tx.Error)
		return out
	}
	out.hash = res.Hash
	if tx.RowsAffected > 0 {
		out.uploaded++
	}
	return out
}

func (r *runner) upload(ctx context.Context, path, filename string) (*imageclient.UploadResult, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, os.ErrNotExist
	}
	var lastErr error
	for attempt := 0; attempt < uploadRetries; attempt++ {
		if r.gap > 0 {
			time.Sleep(r.gap)
		}
		res, err := r.cli.UploadWithSub(ctx, bytes.NewReader(body), filename, r.slot.Preset, r.slot.UploaderSub)
		if err == nil {
			return res, nil
		}
		if stderrors.Is(err, imageclient.ErrQuotaExceeded) || stderrors.Is(err, imageclient.ErrModerationRejected) {
			return nil, err
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt < uploadRetries-1 {
			time.Sleep(time.Duration(min(5<<attempt, 30)) * time.Second)
		}
	}
	return nil, lastErr
}

func (r *runner) ping(ctx context.Context) error {
	if r.cli == nil || len(r.pingHashes) == 0 {
		return nil
	}
	for i := 0; i < len(r.pingHashes); i += 1000 {
		batch := r.pingHashes[i:min(i+1000, len(r.pingHashes))]
		if _, err := r.cli.ReferencePing(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

func (r *runner) run(ctx context.Context, opts Opts, cands []candidate) {
	workers := opts.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(cands) {
		workers = len(cands)
	}

	if workers <= 1 {
		for _, c := range cands {
			if ctx.Err() != nil || r.stats.Quota {
				return
			}
			r.absorb(r.fill(ctx, opts.MirrorDir, c, opts.Apply))
		}
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	in := make(chan candidate)
	out := make(chan charResult, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range in {
				out <- r.fill(ctx, opts.MirrorDir, c, opts.Apply)
			}
		}()
	}
	go func() {
		defer close(in)
		for _, c := range cands {
			select {
			case <-ctx.Done():
				return
			case in <- c:
			}
		}
	}()
	go func() { wg.Wait(); close(out) }()

	done := 0
	for res := range out {
		r.absorb(res)
		done++
		if res.quota {
			cancel()
		}
		if done%500 == 0 {
			slog.Info("getchu-portraits progress", "slot", r.slot.Name, "done", done, "of", len(cands),
				"uploaded", r.stats.Uploaded, "errors", r.stats.Errors)
		}
	}
}

func (r *runner) absorb(res charResult) {
	r.stats.merge(res)
	if res.hash != "" {
		r.pingHashes = append(r.pingHashes, res.hash)
	}
}
