package dlsitemedia

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const (
	defaultTimeout = 60 * time.Second
	metaChunk      = 500
)

type Kinds struct{ Intro, Cover, Screenshot bool }

func (k Kinds) needsMirror() bool { return k.Cover || k.Screenshot }
func (k Kinds) needsImage() bool  { return k.Cover || k.Screenshot }
func (k Kinds) any() bool         { return k.Intro || k.Cover || k.Screenshot }

func ParseKinds(s string) (Kinds, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" {
		return Kinds{Intro: true, Cover: true, Screenshot: true}, nil
	}
	var k Kinds
	for _, part := range strings.Split(s, ",") {
		switch strings.TrimSpace(part) {
		case "intro":
			k.Intro = true
		case "cover":
			k.Cover = true
		case "screenshot", "shot":
			k.Screenshot = true
		case "":
		default:
			return k, fmt.Errorf("unknown kind %q (want intro|cover|screenshot|all)", part)
		}
	}
	if !k.any() {
		return k, fmt.Errorf("no valid kind in %q", s)
	}
	return k, nil
}

type Opts struct {
	Apply        bool
	Kinds        Kinds
	Limit        int
	Offset       int
	DSN          string
	DlsiteDSN    string
	MirrorDir    string
	WorknosOut   string
	CDNMissing   string
	ImageBaseURL string
	UploadGap    time.Duration
}

type counters struct {
	introWritten, introWould, introExists, introNoText int

	coverUploaded, coverWould, coverExists, coverPlaceholder, coverMissing, coverCDNMissing, coverRejected, coverRefused, coverDedup int

	shotUploaded, shotWould, shotExists, shotMissing, shotCDNMissing, shotRejected, shotNoSamples, shotDedup int

	errors int
}

type runner struct {
	db         *gorm.DB
	cli        *imageclient.Client
	sourceID   int16
	gap        time.Duration
	exist      *existing
	c          counters
	pingHashes []string
	touched    []int64
	unmirrored map[string]bool
	cdnMissing map[string]bool
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (map[string]any, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the production run")
	}
	if opts.DlsiteDSN == "" {
		return nil, fmt.Errorf("--dlsite-dsn is required (dlsite staging DB — reads product_json/page_json)")
	}
	if !opts.Kinds.any() {
		return nil, fmt.Errorf("no media kinds selected")
	}
	if opts.WorknosOut != "" && opts.Apply {
		return nil, fmt.Errorf("--worknos-out lists what a dry run would need; drop --apply")
	}
	if opts.Kinds.needsMirror() && opts.MirrorDir == "" {
		return nil, fmt.Errorf("--mirror-dir is required for cover/screenshot (local mirror root <root>/<workno>/<filename>)")
	}
	if opts.CDNMissing != "" && !opts.Kinds.needsMirror() {
		return nil, fmt.Errorf("--cdn-missing applies to cover/screenshot")
	}

	clientCfg := cfg.CatalogImageClient
	needImage := opts.Apply && opts.Kinds.needsImage()
	if needImage && (clientCfg.ClientID == "" || clientCfg.ClientSecret == "") {
		return nil, fmt.Errorf("catalog image client not configured (set KUN_CATALOG_IMAGE_CLIENT_ID/SECRET); refusing to --apply cover/screenshot")
	}

	var cdnMissing map[string]bool
	if opts.CDNMissing != "" {
		var err error
		cdnMissing, err = loadCDNMissing(opts.CDNMissing)
		if err != nil {
			return nil, err
		}
	}

	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	dldb, err := openGorm(opts.DlsiteDSN)
	if err != nil {
		return nil, fmt.Errorf("connect dlsite db: %w", err)
	}
	if sqlDB, e := dldb.DB(); e == nil {
		defer sqlDB.Close()
	}

	reg, err := resolveRegistry(ctx, db)
	if err != nil {
		return nil, err
	}
	cands, err := loadCandidates(ctx, db, reg, opts.Kinds, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}
	workIDs := make([]int64, len(cands))
	for i, c := range cands {
		workIDs[i] = c.WorkID
	}
	exist, err := preloadExisting(ctx, db, workIDs, reg.dlsiteSource, langJa)
	if err != nil {
		return nil, err
	}
	bodylessCands, claimedCands := laneSplit(cands)
	slog.Info("dlsite-media candidates", "candidates", len(cands),
		"bodyless", bodylessCands, "claimed", claimedCands, "apply", opts.Apply,
		"kinds", fmt.Sprintf("intro=%t cover=%t screenshot=%t", opts.Kinds.Intro, opts.Kinds.Cover, opts.Kinds.Screenshot),
		"offset", opts.Offset, "limit", opts.Limit)

	r := &runner{db: db, sourceID: reg.dlsiteSource, gap: opts.UploadGap, exist: exist, cdnMissing: cdnMissing}
	if needImage {
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

	quota := r.process(ctx, opts, cands, dldb)
	if opts.WorknosOut != "" {
		if err := writeWorknos(opts.WorknosOut, r.unmirrored); err != nil {
			return nil, fmt.Errorf("write --worknos-out: %w", err)
		}
	}

	if err := repository.TouchWorks(ctx, r.db, r.touched); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}

	if opts.Apply {
		for _, b := range chunk(r.pingHashes, 1000) {
			if _, err := r.cli.ReferencePing(ctx, b); err != nil {
				slog.Warn("dlsite-media reference-ping failed", "err", err)
			}
		}
	}

	sum := r.summary(opts, len(cands))
	sum["candidates_bodyless"] = bodylessCands
	sum["candidates_claimed"] = claimedCands
	sum["unmirrored_works"] = len(r.unmirrored)
	slog.Info("dlsite-media done", "summary", sum)
	if quota {
		return sum, fmt.Errorf("image quota exceeded — aborted (rerun to resume; idempotent)")
	}
	return sum, nil
}

func (r *runner) process(ctx context.Context, opts Opts, cands []candidate, dldb *gorm.DB) (quota bool) {
	for _, batch := range chunk(cands, metaChunk) {
		if err := ctx.Err(); err != nil {
			return false
		}
		worknos := make([]string, len(batch))
		for i, c := range batch {
			worknos[i] = c.Workno
		}
		metas, err := loadDlsiteMeta(ctx, dldb, worknos)
		if err != nil {
			r.c.errors++
			slog.Error("load dlsite meta chunk", "err", err)
			continue
		}
		for _, c := range batch {
			if err := ctx.Err(); err != nil {
				return false
			}
			m := metas[c.Workno]
			if opts.Kinds.Intro {
				r.writeIntro(ctx, c, m, opts.Apply)
			}
			if opts.Kinds.Cover {
				if r.writeCover(ctx, opts.MirrorDir, c, m, opts.Apply) {
					return true
				}
			}
			if opts.Kinds.Screenshot {
				if r.writeScreenshots(ctx, opts.MirrorDir, c, m, opts.Apply) {
					return true
				}
			}
		}
	}
	return false
}

func (r *runner) summary(opts Opts, candidates int) map[string]any {
	s := map[string]any{
		"apply":      opts.Apply,
		"candidates": candidates,
		"errors":     r.c.errors,
	}
	if opts.Kinds.Intro {
		s["intro"] = map[string]any{
			"written": r.c.introWritten, "would_write": r.c.introWould,
			"already_exists": r.c.introExists, "no_text": r.c.introNoText,
		}
	}
	if opts.Kinds.Cover {
		s["cover"] = map[string]any{
			"uploaded": r.c.coverUploaded, "would_upload": r.c.coverWould,
			"already_exists": r.c.coverExists, "placeholder_skip": r.c.coverPlaceholder,
			"missing_file": r.c.coverMissing, "rejected": r.c.coverRejected,
			"refused_claimed": r.c.coverRefused, "dedup": r.c.coverDedup,
			"cdn_missing": r.c.coverCDNMissing,
		}
	}
	if opts.Kinds.Screenshot {
		s["screenshot"] = map[string]any{
			"uploaded": r.c.shotUploaded, "would_upload": r.c.shotWould,
			"already_exists": r.c.shotExists, "missing_file": r.c.shotMissing,
			"rejected": r.c.shotRejected, "no_samples": r.c.shotNoSamples, "dedup": r.c.shotDedup,
			"cdn_missing": r.c.shotCDNMissing,
		}
	}
	return s
}

func (r *runner) markUnmirrored(workno string) {
	if r.unmirrored == nil {
		r.unmirrored = map[string]bool{}
	}
	r.unmirrored[workno] = true
}

func writeWorknos(path string, set map[string]bool) error {
	worknos := make([]string, 0, len(set))
	for w := range set {
		worknos = append(worknos, w)
	}
	slices.Sort(worknos)
	var b strings.Builder
	for _, w := range worknos {
		b.WriteString(w)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func loadCDNMissing(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --cdn-missing: %w", err)
	}
	out := map[string]bool{}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		workno, filename, ok := strings.Cut(line, "/")
		if !ok || workno == "" || filename == "" || strings.Contains(filename, "/") {
			return nil, fmt.Errorf("line %d: want <workno>/<filename>", i+1)
		}
		out[line] = true
	}
	return out, nil
}

func laneSplit(cands []candidate) (bodyless, claimed int) {
	for _, c := range cands {
		if isBodyless(c.Site) {
			bodyless++
			continue
		}
		claimed++
	}
	return bodyless, claimed
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
