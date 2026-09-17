package dlsitemedia

import (
	"bytes"
	"context"
	stderrors "errors"
	"log/slog"
	"os"
	"time"

	"api/internal/platform/catalog/model"
	"api/pkg/imageclient"

	"gorm.io/gorm/clause"
)

const (
	langJa = "ja"

	coverPreset = "catalog_cover"
	shotPreset  = "catalog_screenshot"

	uploaderSub = "system:dlsite-media-backfill"

	uploadRetries = 6
)

func (r *runner) writeIntro(ctx context.Context, c candidate, m dlsiteMeta, apply bool) {
	if m.Intro == "" {
		r.c.introNoText++
		return
	}
	if r.exist.intro[c.WorkID] {
		r.c.introExists++
		return
	}
	if !apply {
		r.c.introWould++
		return
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "lang"}, {Name: "source_id"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkIntro{
		WorkID: c.WorkID, Lang: langJa, Intro: m.Intro, SourceID: r.sourceID,
	})
	if res.Error != nil {
		r.c.errors++
		slog.Warn("write intro", "work", c.WorkID, "workno", c.Workno, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		r.c.introExists++
		return
	}
	r.exist.intro[c.WorkID] = true
	r.touched = append(r.touched, c.WorkID)
	r.c.introWritten++
}

func (r *runner) writeCover(ctx context.Context, dir string, c candidate, m dlsiteMeta, apply bool) (quota bool) {
	if !isBodyless(c.Site) {
		r.c.coverRefused++
		return false
	}
	if m.CoverFile == "" {
		r.c.coverPlaceholder++
		return false
	}
	if r.exist.cover[c.WorkID] {
		r.c.coverExists++
		return false
	}
	path := mirrorPath(dir, c.Workno, m.CoverFile)
	if !fileExists(path) {
		r.c.coverMissing++
		r.markUnmirrored(c.Workno)
		return false
	}
	if !apply {
		r.c.coverWould++
		return false
	}
	res, upErr := r.upload(ctx, path, m.CoverFile, coverPreset)
	if upErr != nil {
		return r.classifyUpload(upErr, "cover", c, &r.c.coverRejected)
	}
	tx := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "image_hash"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkCover{
		WorkID: c.WorkID, ImageHash: res.Hash, SortOrder: 0, Kind: "main",
		PortraitPinned: false, Sexual: ageToSexual(m.Age), Violence: 0, SourceID: r.sourceID,
	})
	if tx.Error != nil {
		r.c.errors++
		slog.Warn("write cover row", "work", c.WorkID, "err", tx.Error)
		return false
	}
	r.pingHashes = append(r.pingHashes, res.Hash)
	if tx.RowsAffected == 0 {
		r.c.coverDedup++
		return false
	}
	r.exist.cover[c.WorkID] = true
	r.touched = append(r.touched, c.WorkID)
	r.c.coverUploaded++
	return false
}

func (r *runner) writeScreenshots(ctx context.Context, dir string, c candidate, m dlsiteMeta, apply bool) (quota bool) {
	if len(m.SampleFiles) == 0 {
		r.c.shotNoSamples++
		return false
	}
	present := r.exist.shot[c.WorkID]
	for i, fname := range m.SampleFiles {
		if present != nil && present[i] {
			r.c.shotExists++
			continue
		}
		path := mirrorPath(dir, c.Workno, fname)
		if !fileExists(path) {
			r.c.shotMissing++
			r.markUnmirrored(c.Workno)
			continue
		}
		if !apply {
			r.c.shotWould++
			continue
		}
		res, upErr := r.upload(ctx, path, fname, shotPreset)
		if upErr != nil {
			if r.classifyUpload(upErr, "screenshot", c, &r.c.shotRejected) {
				return true
			}
			continue
		}
		tx := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "work_id"}, {Name: "image_hash"}},
			DoNothing: true,
		}).Create(&model.CatalogWorkScreenshot{
			WorkID: c.WorkID, ImageHash: res.Hash, SortOrder: i, Caption: "",
			Sexual: ageToSexual(m.Age), Violence: 0, SourceID: r.sourceID,
		})
		if tx.Error != nil {
			r.c.errors++
			slog.Warn("write screenshot row", "work", c.WorkID, "sort", i, "err", tx.Error)
			continue
		}
		r.pingHashes = append(r.pingHashes, res.Hash)
		if tx.RowsAffected == 0 {
			r.c.shotDedup++
			continue
		}
		if present == nil {
			present = map[int]bool{}
			r.exist.shot[c.WorkID] = present
		}
		present[i] = true
		r.touched = append(r.touched, c.WorkID)
		r.c.shotUploaded++
	}
	return false
}

func (r *runner) upload(ctx context.Context, path, filename, preset string) (*imageclient.UploadResult, error) {
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
		res, err := r.cli.UploadWithSub(ctx, bytes.NewReader(body), filename, preset, uploaderSub)
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

func (r *runner) classifyUpload(err error, kind string, c candidate, rejected *int) (quota bool) {
	switch {
	case stderrors.Is(err, imageclient.ErrQuotaExceeded):
		return true
	case imageclient.IsPermanent(err):
		*rejected++
		slog.Warn(kind+" rejected by the image service", "work", c.WorkID, "workno", c.Workno, "err", err)
		return false
	default:
		r.c.errors++
		slog.Warn("upload "+kind, "work", c.WorkID, "workno", c.Workno, "err", err)
		return false
	}
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}
