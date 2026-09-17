package bangumicovers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const defaultTimeout = 60 * time.Second

type Opts struct {
	Apply          bool
	Limit          int
	Offset         int
	DSN            string
	BangumiMirror  string
	SubjectsOut    string
	ImageBaseURL   string
	UploadGap      time.Duration
	AllowLandscape bool
}

type imageUploader interface {
	UploadWithSub(ctx context.Context, r io.Reader, filename, preset, uploaderSub string) (*imageclient.UploadResult, error)
	ReferencePing(ctx context.Context, hashes []string) (*imageclient.ReferencePingResult, error)
	Health(ctx context.Context) error
}

type counters struct {
	coverUploaded    int
	coverWould       int
	coverExists      int
	coverLandscape   int
	coverLandscapeOK int
	coverNoDims      int
	coverMissing     int
	coverRejected    int
	coverRefused     int
	coverDedup       int
	errors           int
}

type runner struct {
	db         *gorm.DB
	cli        imageUploader
	sourceID   int16
	gap        time.Duration
	exist      map[int64]bool
	pinned     map[int64]bool
	c          counters
	pingHashes []string
	touched    []int64
	unmirrored map[int64]bool
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (map[string]any, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the production run")
	}
	if opts.SubjectsOut != "" && opts.Apply {
		return nil, fmt.Errorf("--subjects-out lists what a dry run would need; drop --apply")
	}
	if opts.BangumiMirror == "" {
		return nil, fmt.Errorf("--bangumi-mirror is required (local mirror root <dir>/<subject_id>/cover.jpg + <dir>/dims.jsonl)")
	}

	clientCfg := cfg.CatalogImageClient
	if opts.Apply && (clientCfg.ClientID == "" || clientCfg.ClientSecret == "") {
		return nil, fmt.Errorf("catalog image client not configured (set KUN_CATALOG_IMAGE_CLIENT_ID/SECRET); refusing to --apply cover upload")
	}

	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	reg, err := resolveRegistry(ctx, db)
	if err != nil {
		return nil, err
	}
	cands, err := loadCandidates(ctx, db, reg, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}
	d, err := loadDims(opts.BangumiMirror)
	if err != nil {
		return nil, err
	}
	workIDs := make([]int64, len(cands))
	for i, c := range cands {
		workIDs[i] = c.WorkID
	}
	exist, err := preloadExistingCovers(ctx, db, workIDs, reg.bangumiSource)
	if err != nil {
		return nil, err
	}
	pinned, err := preloadPinnedCovers(ctx, db, workIDs)
	if err != nil {
		return nil, err
	}
	slog.Info("bangumi-covers candidates", "exact_anchors", len(cands), "dims_rows", len(d.entry),
		"apply", opts.Apply, "offset", opts.Offset, "limit", opts.Limit)

	r := &runner{db: db, sourceID: reg.bangumiSource, gap: opts.UploadGap, exist: exist, pinned: pinned}
	if opts.Apply {
		r.cli = imageclient.New(imageclient.Config{
			BaseURL:      resolveBaseURL(cfg, clientCfg, opts.ImageBaseURL),
			CDNBase:      cfg.ImageService.CDNBase,
			ClientID:     clientCfg.ClientID,
			ClientSecret: clientCfg.ClientSecret,
			Timeout:      defaultTimeout,
		})
		hctx, hcancel := context.WithTimeout(ctx, 5*time.Second)
		defer hcancel()
		if err := r.cli.Health(hctx); err != nil {
			return nil, fmt.Errorf("image_service unreachable at %s: %w", resolveBaseURL(cfg, clientCfg, opts.ImageBaseURL), err)
		}
	}

	quota := r.process(ctx, opts, cands, d)
	if opts.SubjectsOut != "" {
		if err := writeSubjects(opts.SubjectsOut, r.unmirrored); err != nil {
			return nil, fmt.Errorf("write --subjects-out: %w", err)
		}
	}

	if err := repository.TouchWorks(ctx, r.db, r.touched); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}

	if opts.Apply {
		for _, b := range chunk(r.pingHashes, 1000) {
			if _, err := r.cli.ReferencePing(ctx, b); err != nil {
				slog.Warn("bangumi-covers reference-ping failed", "err", err)
			}
		}
	}

	sum := r.summary(opts, len(cands))
	slog.Info("bangumi-covers done", "summary", sum)
	if quota {
		return sum, fmt.Errorf("image quota exceeded — aborted (rerun to resume; idempotent)")
	}
	return sum, nil
}

func (r *runner) process(ctx context.Context, opts Opts, cands []candidate, d *dims) (quota bool) {
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return false
		}
		e, ok := d.entry[c.SubjectID]
		if !ok {
			r.c.coverNoDims++
			if !r.exist[c.WorkID] {
				r.markUnmirrored(c)
			}
			continue
		}
		if !e.portrait() {
			if !opts.AllowLandscape {
				r.c.coverLandscape++
				continue
			}
			r.c.coverLandscapeOK++
		}
		if r.writeCover(ctx, opts.BangumiMirror, c, e, opts.Apply) {
			return true
		}
	}
	return false
}

func (r *runner) summary(opts Opts, candidates int) map[string]any {
	return map[string]any{
		"apply":         opts.Apply,
		"exact_anchors": candidates,
		"cover": map[string]any{
			"uploaded":        r.c.coverUploaded,
			"would_upload":    r.c.coverWould,
			"already_exists":  r.c.coverExists,
			"landscape_skip":  r.c.coverLandscape,
			"landscape_used":  r.c.coverLandscapeOK,
			"no_dims_skip":    r.c.coverNoDims,
			"missing_file":    r.c.coverMissing,
			"rejected":        r.c.coverRejected,
			"refused_claimed": r.c.coverRefused,
			"dedup":           r.c.coverDedup,
		},
		"errors":              r.c.errors,
		"unmirrored_subjects": len(r.unmirrored),
	}
}

func (r *runner) markUnmirrored(c candidate) {
	id, err := strconv.ParseInt(c.SubjectID, 10, 64)
	if err != nil {
		return
	}
	if r.unmirrored == nil {
		r.unmirrored = map[int64]bool{}
	}
	r.unmirrored[id] = true
}

func writeSubjects(path string, set map[int64]bool) error {
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(strconv.FormatInt(id, 10))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func resolveBaseURL(cfg *config.Config, clientCfg config.ImageClientConfig, override string) string {
	if override != "" {
		return override
	}
	if clientCfg.BaseURL != "" {
		return clientCfg.BaseURL
	}
	return fmt.Sprintf("http://%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
}

func chunk[T any](src []T, n int) [][]T {
	var out [][]T
	for i := 0; i < len(src); i += n {
		out = append(out, src[i:min(i+n, len(src))])
	}
	return out
}
