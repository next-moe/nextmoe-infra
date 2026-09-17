package labellogos

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"api/pkg/imageclient"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const logoPreset = "catalog_logo"

const provField = "logo_hash"

type provEntry struct {
	Source string `json:"source"`
	At     string `json:"at"`
}

type imageUploader interface {
	UploadWithSub(ctx context.Context, r io.Reader, filename, preset, uploaderSub string) (*imageclient.UploadResult, error)
	ReferencePing(ctx context.Context, hashes []string) (*imageclient.ReferencePingResult, error)
}

type runner struct {
	db         *gorm.DB
	cli        imageUploader
	source     Source
	mirror     *mirror
	gap        time.Duration
	stats      *Stats
	pingHashes []string
}

type labelResult struct {
	missing, would, uploaded, raced, rejected, errors int
	quota                                             bool
	hash                                              string
}

func (s *Stats) merge(r labelResult) {
	s.Missing += r.missing
	s.Would += r.would
	s.Uploaded += r.uploaded
	s.Raced += r.raced
	s.Rejected += r.rejected
	s.Errors += r.errors
	if r.quota {
		s.Quota = true
	}
}

func (r *runner) fill(ctx context.Context, c candidate, apply bool) labelResult {
	var out labelResult
	path, ok := r.mirror.resolve(c.ExternalID)
	if !ok {
		out.missing++
		return out
	}
	if !apply {
		out.would++
		return out
	}

	res, err := r.upload(ctx, path)
	if err != nil {
		switch {
		case stderrors.Is(err, imageclient.ErrQuotaExceeded):
			slog.Warn("daily image quota exhausted — stopping", "label", c.LabelID)
			out.quota = true
		case imageclient.IsPermanent(err):
			out.rejected++
			slog.Warn("logo rejected by the image service", "source", r.source.Key, "label", c.LabelID, "external_id", c.ExternalID, "err", err)
		default:
			out.errors++
			slog.Warn("upload label logo", "source", r.source.Key, "label", c.LabelID, "external_id", c.ExternalID, "err", err)
		}
		return out
	}

	tx := r.db.WithContext(ctx).Exec(
		`UPDATE catalog_label SET logo_hash = ?, field_provenance = ?, updated_at = now()
		 WHERE id = ? AND logo_hash = '' AND deleted_at IS NULL`,
		res.Hash, mergeProvenance(c.FieldProvenance, r.source.Key, nowUTC()), c.LabelID)
	if tx.Error != nil {
		out.errors++
		slog.Warn("write label logo", "source", r.source.Key, "label", c.LabelID, "err", tx.Error)
		return out
	}
	out.hash = res.Hash
	if tx.RowsAffected > 0 {
		out.uploaded++
	} else {
		out.raced++
	}
	return out
}

func mergeProvenance(cur datatypes.JSON, source, at string) datatypes.JSON {
	doc := map[string]json.RawMessage{}
	if len(cur) > 0 {
		_ = json.Unmarshal(cur, &doc)
	}
	entry, _ := json.Marshal(provEntry{Source: source, At: at})
	var arr []json.RawMessage
	if raw, ok := doc[provField]; ok {
		_ = json.Unmarshal(raw, &arr)
	}
	arr = append([]json.RawMessage{entry}, arr...)
	merged, _ := json.Marshal(arr)
	doc[provField] = merged
	out, _ := json.Marshal(doc)
	return datatypes.JSON(out)
}

func nowUTC() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func (r *runner) upload(ctx context.Context, path string) (*imageclient.UploadResult, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, os.ErrNotExist
	}
	filename := filepath.Base(path)
	var lastErr error
	for attempt := 0; attempt < uploadRetries; attempt++ {
		if r.gap > 0 {
			time.Sleep(r.gap)
		}
		res, err := r.cli.UploadWithSub(ctx, bytes.NewReader(body), filename, logoPreset, r.source.UploaderSub)
		if err == nil {
			return res, nil
		}
		if stderrors.Is(err, imageclient.ErrQuotaExceeded) || imageclient.IsPermanent(err) {
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
			r.absorb(r.fill(ctx, c, opts.Apply))
		}
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	in := make(chan candidate)
	out := make(chan labelResult, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range in {
				out <- r.fill(ctx, c, opts.Apply)
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
			slog.Info("label-logos progress", "source", r.source.Key, "done", done, "of", len(cands),
				"uploaded", r.stats.Uploaded, "errors", r.stats.Errors)
		}
	}
}

func (r *runner) absorb(res labelResult) {
	r.stats.merge(res)
	if res.hash != "" {
		r.pingHashes = append(r.pingHashes, res.hash)
	}
}
