package bangumicovers

import (
	"bytes"
	"context"
	stderrors "errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"api/internal/platform/catalog/model"
	"api/pkg/imageclient"

	"gorm.io/gorm/clause"
)

const (
	coverPreset = "catalog_cover"

	uploaderSub = "system:bangumi-cover-backfill"

	uploadRetries = 6
)

func (r *runner) writeCover(ctx context.Context, mirrorRoot string, c candidate, e dimsEntry, apply bool) (quota bool) {
	if !isBodyless(c.Site) {
		r.c.coverRefused++
		return false
	}
	if r.exist[c.WorkID] {
		r.c.coverExists++
		return false
	}
	path := coverPath(mirrorRoot, c.SubjectID, e)
	if !fileExists(path) {
		r.c.coverMissing++
		r.markUnmirrored(c)
		return false
	}
	if !apply {
		r.c.coverWould++
		return false
	}
	res, upErr := r.upload(ctx, path)
	if upErr != nil {
		return r.classifyUpload(upErr, c)
	}
	tx := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "image_hash"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkCover{
		WorkID: c.WorkID, ImageHash: res.Hash, SortOrder: 0, Kind: "main",
		PortraitPinned: e.portrait() && !r.pinned[c.WorkID], Sexual: 0, Violence: 0, SourceID: r.sourceID,
	})
	if tx.Error != nil {
		r.c.errors++
		slog.Warn("write bangumi cover row", "work", c.WorkID, "subject", c.SubjectID, "err", tx.Error)
		return false
	}
	r.pingHashes = append(r.pingHashes, res.Hash)
	if tx.RowsAffected == 0 {
		r.c.coverDedup++
		return false
	}
	r.exist[c.WorkID] = true
	r.touched = append(r.touched, c.WorkID)
	r.c.coverUploaded++
	return false
}

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
		if attempt < uploadRetries-1 {
			time.Sleep(time.Duration(min(5<<attempt, 30)) * time.Second)
		}
	}
	return nil, lastErr
}

func (r *runner) classifyUpload(err error, c candidate) (quota bool) {
	switch {
	case stderrors.Is(err, imageclient.ErrQuotaExceeded):
		return true
	case stderrors.Is(err, imageclient.ErrModerationRejected):
		r.c.coverRejected++
		slog.Warn("bangumi cover rejected by moderation", "work", c.WorkID, "subject", c.SubjectID)
		return false
	default:
		r.c.errors++
		slog.Warn("upload bangumi cover", "work", c.WorkID, "subject", c.SubjectID, "err", err)
		return false
	}
}
