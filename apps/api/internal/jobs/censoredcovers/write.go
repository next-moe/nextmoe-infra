package censoredcovers

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net/http"
	"time"

	"api/internal/platform/catalog/model"
	"api/pkg/imageclient"

	"gorm.io/gorm/clause"
)

const (
	coverPreset = "catalog_cover"

	uploaderSub = "system:censored-cover"

	coverKind = "main"

	uploadRetries = 6

	downloadRetries = 3

	maxImageBytes = 16 << 20

	downloadTimeout = 60 * time.Second

	maxSourceRows = 4

	userAgent = "nextmoe-infra/backfill-censored-covers (+https://www.kungal.com)"
)

func (r *runner) fill(ctx context.Context, cand candidate) {
	rows, err := loadExplicitRows(ctx, r.db, cand.WorkID)
	if err != nil {
		r.stats.Errors++
		slog.Warn("load explicit rows", "work", cand.WorkID, "err", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	r.stats.Planned++
	best := r.bestSource(ctx, cand.WorkID, rows)
	if best == nil {
		r.stats.Errors++
		return
	}
	ghost, w, h, err := ghostBytes(best)
	if err != nil {
		r.stats.Errors++
		slog.Warn("derive ghost", "work", cand.WorkID, "err", err)
		return
	}
	res, err := r.upload(ctx, ghost, fmt.Sprintf("censored-%d.jpg", cand.WorkID))
	if err != nil {
		if r.classify(err, cand.WorkID) {
			r.stats.Quota = true
		}
		return
	}
	tx := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "image_hash"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkCover{
		WorkID: cand.WorkID, ImageHash: res.Hash, SortOrder: 0, Kind: coverKind,
		SourceID: r.sourceID,
	})
	if tx.Error != nil {
		r.stats.Errors++
		slog.Warn("write ghost cover row", "work", cand.WorkID, "err", tx.Error)
		return
	}
	r.pingHashes = append(r.pingHashes, res.Hash)
	if tx.RowsAffected == 0 {
		r.stats.Dedup++
		return
	}
	r.touched = append(r.touched, cand.WorkID)
	r.stats.Uploaded++
	slog.Info("ghost staged", "work", cand.WorkID, "hash", res.Hash, "dims", fmt.Sprintf("%dx%d", w, h))
}

// bestSource downloads up to maxSourceRows explicit art rows and picks the one
// worth ghosting: portrait beats landscape (the ghost serves the portrait
// slot), sharper beats smaller within a shape.
func (r *runner) bestSource(ctx context.Context, workID int64, rows []explicitRow) []byte {
	var bestBytes []byte
	var bestW, bestH int
	for i, row := range rows {
		if i == maxSourceRows || ctx.Err() != nil {
			break
		}
		body, err := r.download(ctx, imageclient.MainURL(r.cdnBase, row.ImageHash, "webp"))
		if err != nil {
			slog.Warn("download source art", "work", workID, "hash", row.ImageHash, "err", err)
			continue
		}
		cfgImg, _, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			slog.Warn("decode source art", "work", workID, "hash", row.ImageHash, "err", err)
			continue
		}
		if bestBytes == nil || betterSource(cfgImg.Width, cfgImg.Height, bestW, bestH) {
			bestBytes, bestW, bestH = body, cfgImg.Width, cfgImg.Height
		}
	}
	return bestBytes
}

func betterSource(w, h, curW, curH int) bool {
	if p, cp := h > w, curH > curW; p != cp {
		return p
	}
	return w*h > curW*curH
}

func (r *runner) download(ctx context.Context, src string) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	var lastErr error
	for attempt := 0; attempt < downloadRetries; attempt++ {
		if attempt > 0 && !sleepCtx(ctx, time.Duration(5<<attempt)*time.Second) {
			return nil, ctx.Err()
		}
		body, err := fetch(ctx, client, src)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
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

func (r *runner) upload(ctx context.Context, body []byte, filename string) (*imageclient.UploadResult, error) {
	var lastErr error
	for attempt := 0; attempt < uploadRetries; attempt++ {
		if r.gap > 0 && !sleepCtx(ctx, r.gap) {
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

func (r *runner) classify(err error, workID int64) (quota bool) {
	switch {
	case stderrors.Is(err, imageclient.ErrQuotaExceeded):
		slog.Warn("daily image quota exhausted — stopping", "work", workID)
		return true
	case stderrors.Is(err, imageclient.ErrModerationRejected):
		r.stats.Rejected++
		slog.Warn("ghost rejected by moderation", "work", workID)
		return false
	default:
		r.stats.Errors++
		slog.Warn("upload ghost", "work", workID, "err", err)
		return false
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
