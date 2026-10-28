package officialcovers

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/pkg/config"
	"api/pkg/imageclient"
	"api/pkg/imageshrink"

	"gorm.io/gorm"
)

const (
	coverPreset = "catalog_cover"
	uploaderSub = "system:official-cover-backfill"
)

type Opts struct {
	DSN          string
	Apply        bool
	IDs          []int64
	Limit        int
	ImageBaseURL string
	UploadGap    time.Duration
	VNDBBase     string
	BgmBase      string
	BgmToken     string
}

type Stats struct {
	Candidates int
	VNDBDig    int
	VNDBNone   int
	Bgm        int
	BgmNone    int
	Uploaded   int
	Relabeled  int
	Dedup      int
	Unpinned   int
	Errors     int
	Quota      bool
}

func (s Stats) String() string {
	return fmt.Sprintf("candidates=%d vndb_dig=%d vndb_none=%d bgm=%d bgm_none=%d uploaded=%d relabeled=%d dedup=%d unpinned=%d errors=%d quota=%t",
		s.Candidates, s.VNDBDig, s.VNDBNone, s.Bgm, s.BgmNone, s.Uploaded, s.Relabeled, s.Dedup, s.Unpinned, s.Errors, s.Quota)
}

type planned struct {
	WorkID   int64
	Source   int16
	Kind     string
	URL      string
	Sexual   int16
	Violence int16
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("--dsn is REQUIRED; refusing to guess a catalog database")
	}
	img := cfg.CatalogImageClient
	if opts.Apply && (img.ClientID == "" || img.ClientSecret == "") {
		return nil, fmt.Errorf("catalog image client not configured (KUN_CATALOG_IMAGE_CLIENT_ID/SECRET); refusing to --apply")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	cands, err := loadCandidates(ctx, db, opts.IDs)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}
	stats := &Stats{Candidates: len(cands)}
	up := &upstream{vndbBase: or(opts.VNDBBase, defaultVNDBBase), bgmBase: or(opts.BgmBase, defaultBgmBase),
		bgmToken: opts.BgmToken, http: &http.Client{Timeout: 60 * time.Second}}
	plan, err := buildPlan(ctx, up, cands, stats)
	if err != nil {
		return stats, err
	}
	for _, p := range plan {
		slog.Info("planned", "work", p.WorkID, "source", p.Source, "kind", p.Kind, "sexual", p.Sexual, "url", p.URL)
	}
	if !opts.Apply {
		slog.Info("official-covers done (dry run)", "result", stats.String())
		return stats, nil
	}
	if opts.Limit > 0 && len(plan) > opts.Limit {
		plan = plan[:opts.Limit]
	}

	base := opts.ImageBaseURL
	if base == "" {
		base = img.BaseURL
	}
	if base == "" {
		base = fmt.Sprintf("http://%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
	}
	w := &writer{db: db, up: up, stats: stats, gap: opts.UploadGap, cli: imageclient.New(imageclient.Config{
		BaseURL: base, CDNBase: cfg.ImageService.CDNBase,
		ClientID: img.ClientID, ClientSecret: img.ClientSecret, Timeout: 60 * time.Second,
	})}
	if err := w.cli.Health(ctx); err != nil {
		return stats, fmt.Errorf("image_service unreachable at %s: %w", base, err)
	}
	for _, p := range plan {
		if ctx.Err() != nil || stats.Quota {
			break
		}
		w.fill(ctx, p)
	}
	if err := repository.TouchWorks(ctx, db, w.touched); err != nil {
		return stats, fmt.Errorf("touch works: %w", err)
	}
	if len(w.hashes) > 0 {
		if _, err := w.cli.ReferencePing(ctx, w.hashes); err != nil {
			slog.Warn("refping fresh hashes", "err", err)
		}
	}
	slog.Info("official-covers done", "result", stats.String())
	if stats.Quota {
		return stats, fmt.Errorf("image quota exceeded — rerun to resume; the job is idempotent")
	}
	return stats, nil
}

func buildPlan(ctx context.Context, up *upstream, cands []candidate, stats *Stats) ([]planned, error) {
	var vns []string
	for _, c := range cands {
		if c.VNDBID != "" {
			vns = append(vns, c.VNDBID)
		}
	}
	digs, err := up.digitalCovers(ctx, vns)
	if err != nil {
		return nil, err
	}
	var plan []planned
	for _, c := range cands {
		if c.VNDBID != "" {
			if d := digs[c.VNDBID]; d != nil {
				stats.VNDBDig++
				plan = append(plan, planned{WorkID: c.WorkID, Source: model.SourceVNDB, Kind: "dig", URL: d.URL,
					Sexual: ratingLevel(d.Sexual), Violence: ratingLevel(d.Violence)})
			} else {
				stats.VNDBNone++
			}
		}
		if c.BgmID == "" {
			continue
		}
		u, err := up.subjectCover(ctx, c.BgmID)
		if err != nil {
			return nil, err
		}
		if u == "" {
			stats.BgmNone++
			continue
		}
		stats.Bgm++
		plan = append(plan, planned{WorkID: c.WorkID, Source: model.SourceBangumi, Kind: "main", URL: u})
	}
	return plan, nil
}

type writer struct {
	db      *gorm.DB
	up      *upstream
	cli     *imageclient.Client
	stats   *Stats
	gap     time.Duration
	last    time.Time
	touched []int64
	hashes  []string
}

func (w *writer) fill(ctx context.Context, p planned) {
	body, err := w.up.download(ctx, p.URL)
	if err == nil {
		var name string
		body, name, err = imageshrink.Shrink(body, filename(p.URL))
		if err == nil {
			err = w.write(ctx, p, body, name)
		}
	}
	switch {
	case err == nil:
	case stderrors.Is(err, imageclient.ErrQuotaExceeded):
		w.stats.Quota = true
		slog.Warn("daily image quota exhausted — stopping", "work", p.WorkID)
	default:
		w.stats.Errors++
		slog.Warn("official cover", "work", p.WorkID, "url", p.URL, "err", err)
	}
}

// Machine pins (repincovers, the importers' first-portrait pin) outrank the
// sharpest portrait, so a freshly added high-resolution cover stays behind the
// ~256px VNDB thumbnail they pinned unless they are lifted. Editor pins stay.
func (w *writer) write(ctx context.Context, p planned, body []byte, name string) error {
	if wait := w.gap - time.Since(w.last); wait > 0 && !sleepCtx(ctx, wait) {
		return ctx.Err()
	}
	w.last = time.Now()
	res, err := w.cli.UploadWithSub(ctx, bytes.NewReader(body), name, coverPreset, uploaderSub)
	if err != nil {
		return err
	}
	w.hashes = append(w.hashes, res.Hash)
	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var held []model.CatalogWorkCover
		if err := tx.Where("work_id = ? AND image_hash = ?", p.WorkID, res.Hash).Find(&held).Error; err != nil {
			return err
		}
		switch {
		case len(held) == 0:
			if err := tx.Create(&model.CatalogWorkCover{
				WorkID: p.WorkID, ImageHash: res.Hash, Kind: p.Kind, Sexual: p.Sexual, Violence: p.Violence, SourceID: p.Source,
			}).Error; err != nil {
				return err
			}
			w.stats.Uploaded++
		// The first production run ended dedup=31: the legacy upload was the
		// official image byte for byte, so the insert hit uq_catalog_work_cover
		// and the work kept no official row — and its machine-pinned upscale.
		// Such a row is re-sourced; one an editor pinned is theirs and stays.
		case model.IsUploadedCoverSource(held[0].SourceID) &&
			!(held[0].SourceID == model.SourceCurated && held[0].PortraitPinned):
			if err := tx.Model(&held[0]).Updates(map[string]any{"source_id": p.Source, "kind": p.Kind}).Error; err != nil {
				return err
			}
			w.stats.Relabeled++
		default:
			w.stats.Dedup++
			return nil
		}
		w.touched = append(w.touched, p.WorkID)
		if res.Width <= 0 || !isPortrait(res.Width, res.Height) {
			return nil
		}
		lift := tx.Exec(`UPDATE catalog_work_cover SET portrait_pinned = false, updated_at = now()
			WHERE work_id = ? AND portrait_pinned AND NOT (`+editspec.CuratedLaneSQL("source_id")+`)
			  AND (`+editspec.HumanFieldProvenanceSQL("field_provenance", "portrait_pinned")+`) IS NOT TRUE`, p.WorkID)
		w.stats.Unpinned += int(lift.RowsAffected)
		return lift.Error
	})
}

func filename(raw string) string {
	if base := path.Base(raw); path.Ext(base) != "" {
		return base
	}
	return "cover.jpg"
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func itoa(v int16) string { return strconv.Itoa(int(v)) }
