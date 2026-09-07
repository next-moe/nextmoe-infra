package vndbcovers

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"api/internal/platform/catalog/model"
	"api/pkg/imageclient"
	"api/pkg/imageshrink"

	"gorm.io/gorm/clause"
)

const (
	coverPreset = "catalog_cover"

	uploaderSub = "system:vndb-cover-backfill"

	coverKind = "main"

	uploadRetries = 6

	downloadRetries = 3

	maxImageBytes = 16 << 20

	downloadTimeout = 60 * time.Second

	defaultCoverFilename = "cover.jpg"
)

func (r *runner) fill(ctx context.Context, row planRow) {
	body, filename, err := r.download(ctx, row.Img.URL)
	if err != nil {
		r.bump(func(s *Stats) { s.Errors++ })
		slog.Warn("download vndb cover", "work", row.WorkID, "vn", row.VNDBID, "url", row.Img.URL, "err", err)
		return
	}
	body, filename, err = imageshrink.Shrink(body, filename)
	if err != nil {
		r.bump(func(s *Stats) { s.Errors++ })
		slog.Warn("shrink vndb cover", "work", row.WorkID, "vn", row.VNDBID, "err", err)
		return
	}
	res, err := r.upload(ctx, body, filename)
	if err != nil {
		if r.classify(err, row) {
			r.bump(func(s *Stats) { s.Quota = true })
		}
		return
	}
	// The first full run ended uploaded=1,725 dedup=46,630: the image service
	// dedups uploads by content, and for most candidates the legacy wiki row
	// (source curated/upscale, kind '') already carried the byte-identical
	// official image. DO NOTHING left those rows curated-sourced — the works
	// stayed candidates, and the planned curated purge would have deleted the
	// official bytes with them. A conflict therefore re-sources the identical
	// row to vndb and normalizes the legacy '' kind; the WHERE keeps re-runs
	// idempotent (rows already vndb-sourced count as dedup, not uploads).
	//
	// The re-run then ended dedup=26, and those 26 works were the same story a
	// second time with a different row: vndb's own main cover is byte-identical
	// to a vndb *package* row we already held, so the source matched, the WHERE
	// held the update back, and the work kept only pkg* rows — which the cover
	// picker vetoes as a family. Matching on kind as well relabels that row to
	// what vndb actually serves it as, and stays idempotent because the second
	// pass then agrees on both columns.
	tx := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "image_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_id", "kind", "portrait_pinned", "sexual", "violence", "updated_at"}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "catalog_work_cover.source_id <> excluded.source_id OR catalog_work_cover.kind <> excluded.kind"},
		}},
	}).Create(&model.CatalogWorkCover{
		WorkID: row.WorkID, ImageHash: res.Hash, SortOrder: 0, Kind: coverKind,
		PortraitPinned: portrait(row.Img.Dims),
		Sexual:         ratingLevel(row.Img.Sexual),
		Violence:       ratingLevel(row.Img.Violence),
		SourceID:       r.sourceID,
	})
	if tx.Error != nil {
		r.bump(func(s *Stats) { s.Errors++ })
		slog.Warn("write vndb cover row", "work", row.WorkID, "vn", row.VNDBID, "err", tx.Error)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pingHashes = append(r.pingHashes, res.Hash)
	if tx.RowsAffected == 0 {
		r.stats.Dedup++
		return
	}
	r.touched = append(r.touched, row.WorkID)
	r.stats.Uploaded++
}

func (r *runner) download(ctx context.Context, src string) ([]byte, string, error) {
	if body, ok := r.readMirror(src); ok {
		r.bump(func(s *Stats) { s.Local++ })
		return body, coverFilename(src), nil
	}
	client := &http.Client{Timeout: downloadTimeout}
	var lastErr error
	for attempt := 0; attempt < downloadRetries; attempt++ {
		if attempt > 0 && !sleepCtx(ctx, time.Duration(5<<attempt)*time.Second) {
			return nil, "", ctx.Err()
		}
		body, err := fetch(ctx, client, src)
		if err == nil {
			return body, coverFilename(src), nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
	}
	return nil, "", lastErr
}

func fetch(ctx context.Context, client *http.Client, src string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image cdn %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("image cdn returned an empty body")
	}
	if len(body) > maxImageBytes {
		return nil, fmt.Errorf("image exceeds %d bytes", maxImageBytes)
	}
	return body, nil
}

func (r *runner) readMirror(src string) ([]byte, bool) {
	if r.imageDir == "" {
		return nil, false
	}
	u, err := url.Parse(src)
	if err != nil {
		return nil, false
	}
	rel := strings.TrimPrefix(path.Clean(u.Path), "/")
	if rel == "" || rel == "." {
		return nil, false
	}
	body, err := os.ReadFile(filepath.Join(r.imageDir, filepath.FromSlash(rel)))
	if err != nil || len(body) == 0 || len(body) > maxImageBytes {
		return nil, false
	}
	return body, true
}

func coverFilename(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return defaultCoverFilename
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" || path.Ext(base) == "" {
		return defaultCoverFilename
	}
	return base
}

func (r *runner) upload(ctx context.Context, body []byte, filename string) (*imageclient.UploadResult, error) {
	var lastErr error
	for attempt := 0; attempt < uploadRetries; attempt++ {
		if !r.pace(ctx) {
			return nil, ctx.Err()
		}
		res, err := r.cli.UploadWithSub(ctx, bytes.NewReader(body), filename, coverPreset, uploaderSub)
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
		if attempt < uploadRetries-1 && !sleepCtx(ctx, time.Duration(min(5<<attempt, 30))*time.Second) {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// The gap is a single global spacing shared by every worker, so raising
// --workers never raises the request rate against the image service beyond
// 1/gap; workers only overlap download+shrink time.
func (r *runner) pace(ctx context.Context) bool {
	if r.gap <= 0 {
		return ctx.Err() == nil
	}
	r.mu.Lock()
	next := r.paceLast.Add(r.gap)
	if now := time.Now(); next.Before(now) {
		next = now
	}
	r.paceLast = next
	r.mu.Unlock()
	if wait := time.Until(next); wait > 0 {
		return sleepCtx(ctx, wait)
	}
	return ctx.Err() == nil
}

func (r *runner) classify(err error, row planRow) (quota bool) {
	switch {
	case stderrors.Is(err, imageclient.ErrQuotaExceeded):
		slog.Warn("daily image quota exhausted — stopping", "work", row.WorkID)
		return true
	case stderrors.Is(err, imageclient.ErrModerationRejected):
		r.stats.Rejected++
		slog.Warn("vndb cover rejected by moderation", "work", row.WorkID, "vn", row.VNDBID)
		return false
	default:
		r.stats.Errors++
		slog.Warn("upload vndb cover", "work", row.WorkID, "vn", row.VNDBID, "err", err)
		return false
	}
}
