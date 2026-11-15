package aicovers

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const (
	coverPreset    = "catalog_cover"
	uploaderSub    = "system:import-ai-covers"
	uploadRetries  = 6
	requestTimeout = 60 * time.Second
)

type Opts struct {
	DSN          string
	Dir          string
	Apply        bool
	Limit        int
	ImageBaseURL string
	UploadGap    time.Duration
	Replace      bool
	DropFile     string
}

type Stats struct {
	Files      int
	BadName    int
	Imported   int
	NoOrigin   int
	Candidates int
	WorkGone   int
	Uploaded   int
	Inserted   int
	Pinned     int
	EditorPin  int
	Rejected   int
	Errors     int
	QuotaBreak bool
	Replaced   int
	Dropped    int
}

func (s Stats) String() string {
	return fmt.Sprintf("files=%d bad_name=%d already_imported=%d no_origin=%d work_gone=%d candidates=%d uploaded=%d inserted=%d replaced=%d pinned=%d editor_pin_kept=%d dropped=%d rejected=%d errors=%d quota=%t",
		s.Files, s.BadName, s.Imported, s.NoOrigin, s.WorkGone, s.Candidates, s.Uploaded, s.Inserted, s.Replaced, s.Pinned, s.EditorPin,
		s.Dropped, s.Rejected, s.Errors, s.QuotaBreak)
}

type product struct {
	path       string
	workID     int64
	originHash string
}

type origin struct {
	Kind      string `gorm:"column:kind"`
	Sexual    int16  `gorm:"column:sexual"`
	Violence  int16  `gorm:"column:violence"`
	WorkLive  bool   `gorm:"column:work_live"`
	Imported  bool   `gorm:"column:imported"`
	EditorPin bool   `gorm:"column:editor_pin"`
}

type runner struct {
	db      *gorm.DB
	cli     *imageclient.Client
	gap     time.Duration
	replace bool
	stats   *Stats
	touched []int64
	fresh   []string
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("--dsn is REQUIRED; refusing to guess a catalog database")
	}
	if opts.Dir == "" && opts.DropFile == "" {
		return nil, fmt.Errorf("--dir or --drop is REQUIRED")
	}
	var products []product
	var bad int
	var err error
	if opts.Dir != "" {
		if products, bad, err = listProducts(opts.Dir); err != nil {
			return nil, err
		}
	}
	var drops []int64
	if opts.DropFile != "" {
		if drops, err = readWorkIDs(opts.DropFile); err != nil {
			return nil, err
		}
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	r := &runner{db: db, gap: opts.UploadGap, replace: opts.Replace, stats: &Stats{Files: len(products), BadName: bad}}
	for _, id := range drops {
		if err := r.drop(ctx, id, opts.Apply); err != nil {
			r.stats.Errors++
			slog.Warn("drop ai cover", "work", id, "err", err)
		}
	}
	if opts.Apply && len(products) > 0 {
		if r.cli, err = newImageClient(ctx, cfg, opts.ImageBaseURL); err != nil {
			return r.stats, err
		}
	}
	acted := 0
	for _, p := range products {
		if ctx.Err() != nil || r.stats.QuotaBreak || (opts.Limit > 0 && acted >= opts.Limit) {
			break
		}
		if r.handle(ctx, p, opts.Apply) {
			acted++
		}
	}
	if err := r.finish(ctx); err != nil {
		return r.stats, err
	}
	return r.stats, nil
}

func (r *runner) handle(ctx context.Context, p product, apply bool) (acted bool) {
	o, err := r.origin(ctx, p)
	if err != nil {
		r.stats.Errors++
		slog.Warn("load origin cover", "work", p.workID, "err", err)
		return false
	}
	switch {
	case o == nil:
		r.stats.NoOrigin++
		return false
	case !o.WorkLive:
		r.stats.WorkGone++
		return false
	case o.Imported && (!r.replace || o.EditorPin):
		r.stats.Imported++
		return false
	}
	r.stats.Candidates++
	if !apply {
		return true
	}
	if err := r.importOne(ctx, p, *o); err != nil {
		if stderrors.Is(err, imageclient.ErrModerationRejected) {
			r.stats.Rejected++
		} else {
			r.stats.Errors++
		}
		slog.Warn("import ai cover", "work", p.workID, "err", err)
	}
	return true
}

func (r *runner) drop(ctx context.Context, workID int64, apply bool) error {
	var editorPin bool
	if err := r.db.WithContext(ctx).Raw(fmt.Sprintf(`
		SELECT EXISTS (SELECT 1 FROM catalog_work_cover h WHERE h.work_id = ? AND h.portrait_pinned AND (%s OR %s))`,
		editspec.CuratedLaneSQL("h.source_id"), editspec.HumanFieldProvenanceSQL("h.field_provenance", "portrait_pinned")),
		workID).Scan(&editorPin).Error; err != nil {
		return err
	}
	if editorPin {
		r.stats.EditorPin++
		return nil
	}
	if !apply {
		var n int64
		if err := r.db.WithContext(ctx).Model(&model.CatalogWorkCover{}).
			Where("work_id = ? AND source_id = ?", workID, model.SourceAIRedraw).Count(&n).Error; err != nil {
			return err
		}
		r.stats.Dropped += int(n)
		return nil
	}
	res := r.db.WithContext(ctx).Exec(`DELETE FROM catalog_work_cover WHERE work_id = ? AND source_id = ?`,
		workID, model.SourceAIRedraw)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		r.stats.Dropped += int(res.RowsAffected)
		r.touched = append(r.touched, workID)
	}
	return nil
}

func readWorkIDs(path string) ([]int64, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, f := range strings.Fields(string(body)) {
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("drop list: %q is not a work id", f)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func listProducts(dir string) ([]product, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	var out []product
	bad := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		id, hash, found := strings.Cut(base, "__")
		workID, err := strconv.ParseInt(id, 10, 64)
		if !found || err != nil || hash == "" {
			bad++
			continue
		}
		out = append(out, product{path: filepath.Join(dir, e.Name()), workID: workID, originHash: hash})
	}
	slices.SortFunc(out, func(a, b product) int { return int(a.workID - b.workID) })
	return out, bad, nil
}

func (r *runner) origin(ctx context.Context, p product) (*origin, error) {
	var rows []origin
	err := r.db.WithContext(ctx).Raw(fmt.Sprintf(`
		SELECT c.kind, c.sexual, c.violence,
		       w.deleted_at IS NULL AND w.status <> ? AS work_live,
		       EXISTS (SELECT 1 FROM catalog_work_cover a WHERE a.work_id = c.work_id AND a.source_id = ?) AS imported,
		       EXISTS (SELECT 1 FROM catalog_work_cover h WHERE h.work_id = c.work_id AND h.portrait_pinned
		               AND (%s OR %s)) AS editor_pin
		FROM catalog_work_cover c
		JOIN catalog_work w ON w.id = c.work_id
		WHERE c.work_id = ? AND c.image_hash = ?`,
		editspec.CuratedLaneSQL("h.source_id"), editspec.HumanFieldProvenanceSQL("h.field_provenance", "portrait_pinned")),
		model.WorkStatusMerged, model.SourceAIRedraw, p.workID, p.originHash).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (r *runner) importOne(ctx context.Context, p product, o origin) error {
	body, err := os.ReadFile(p.path)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("empty product file")
	}
	res, err := r.upload(ctx, body, filepath.Base(p.path))
	if err != nil {
		return err
	}
	r.stats.Uploaded++
	r.fresh = append(r.fresh, res.Hash)

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var next int
		if err := tx.Raw(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM catalog_work_cover WHERE work_id = ?`,
			p.workID).Scan(&next).Error; err != nil {
			return err
		}
		if o.Imported {
			if err := tx.Exec(`DELETE FROM catalog_work_cover WHERE work_id = ? AND source_id = ?`,
				p.workID, model.SourceAIRedraw).Error; err != nil {
				return err
			}
			r.stats.Replaced++
		}
		row := model.CatalogWorkCover{
			WorkID: p.workID, ImageHash: res.Hash, SortOrder: next, Kind: o.Kind,
			Sexual: o.Sexual, Violence: o.Violence, SourceID: model.SourceAIRedraw,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		r.stats.Inserted++
		r.touched = append(r.touched, p.workID)
		if o.EditorPin {
			r.stats.EditorPin++
			return nil
		}
		if err := tx.Exec(`UPDATE catalog_work_cover SET portrait_pinned = false, updated_at = now()
			WHERE work_id = ? AND portrait_pinned AND id <> ?`, p.workID, row.ID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE catalog_work_cover SET portrait_pinned = true, updated_at = now() WHERE id = ?`,
			row.ID).Error; err != nil {
			return err
		}
		r.stats.Pinned++
		return nil
	})
}

func (r *runner) upload(ctx context.Context, body []byte, filename string) (*imageclient.UploadResult, error) {
	var lastErr error
	for attempt := 0; attempt < uploadRetries; attempt++ {
		if r.gap > 0 {
			time.Sleep(r.gap)
		}
		res, err := r.cli.UploadWithSub(ctx, bytes.NewReader(body), filename, coverPreset, uploaderSub)
		if err == nil {
			return res, nil
		}
		if stderrors.Is(err, imageclient.ErrQuotaExceeded) {
			r.stats.QuotaBreak = true
			return nil, err
		}
		if stderrors.Is(err, imageclient.ErrModerationRejected) || ctx.Err() != nil {
			return nil, err
		}
		lastErr = err
		if attempt < uploadRetries-1 {
			time.Sleep(time.Duration(min(5<<attempt, 30)) * time.Second)
		}
	}
	return nil, lastErr
}

func (r *runner) finish(ctx context.Context) error {
	if err := repository.TouchWorks(ctx, r.db, r.touched); err != nil {
		return fmt.Errorf("touch works: %w", err)
	}
	for i := 0; i < len(r.fresh); i += 1000 {
		if _, err := r.cli.ReferencePing(ctx, r.fresh[i:min(i+1000, len(r.fresh))]); err != nil {
			slog.Warn("refping fresh hashes", "err", err)
			return nil
		}
	}
	return nil
}

func newImageClient(ctx context.Context, cfg *config.Config, baseOverride string) (*imageclient.Client, error) {
	c := cfg.CatalogImageClient
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("catalog image client credentials are not configured")
	}
	base := baseOverride
	if base == "" {
		base = c.BaseURL
	}
	if base == "" {
		base = fmt.Sprintf("http://%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
	}
	cli := imageclient.New(imageclient.Config{
		BaseURL: base, CDNBase: cfg.ImageService.CDNBase,
		ClientID: c.ClientID, ClientSecret: c.ClientSecret, Timeout: requestTimeout,
	})
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := cli.Health(hctx); err != nil {
		return nil, fmt.Errorf("image_service unreachable at %s: %w", base, err)
	}
	return cli, nil
}
