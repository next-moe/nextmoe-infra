// Package figurecutout writes the background-removed character standing art
// produced by scripts/character-cutout back into catalog_character.
//
// The swap is in place: figure_hash starts pointing at the cut-out and the
// white-plate original moves to figure_source_hash. Consumers need no change —
// transparency composited onto a light card looks exactly like the white plate
// it replaced — and the original survives so a better model can be re-run
// without going back to Getchu.
package figurecutout

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const (
	uploadRetries = 6
	clientTimeout = 60 * time.Second
)

// Slot names which of the two character image columns a run swaps. The bust
// slot renders through a `cover` preset, so its cutouts must keep the source
// framing (scripts/character-cutout/cutout.py --no-crop) — a bounding-box crop
// moves where the cover crop lands and can take the head off.
type Slot struct {
	Name        string
	Column      string
	SourceCol   string
	Preset      string
	UploaderSub string
}

var (
	SlotFigure = Slot{
		Name: "figure", Column: "figure_hash", SourceCol: "figure_source_hash",
		Preset: "character_figure", UploaderSub: "system:figure-cutout",
	}
	SlotBust = Slot{
		Name: "bust", Column: "image_hash", SourceCol: "image_source_hash",
		Preset: "character", UploaderSub: "system:bust-cutout",
	}
)

func ParseSlot(name string) (Slot, error) {
	switch name {
	case SlotFigure.Name:
		return SlotFigure, nil
	case SlotBust.Name:
		return SlotBust, nil
	default:
		return Slot{}, fmt.Errorf("unknown --slot %q; want %q (figure_hash) or %q (image_hash)",
			name, SlotFigure.Name, SlotBust.Name)
	}
}

type Opts struct {
	DSN       string
	Dir       string
	Slot      Slot
	Apply     bool
	Limit     int
	Workers   int
	UploadGap time.Duration
	ImageBase string
}

type entry struct {
	Hash       string   `json:"hash"`
	File       string   `json:"file"`
	Divergence float64  `json:"divergence"`
	Flagged    []string `json:"flagged"`
	Error      string   `json:"error"`
}

type Stats struct {
	Manifest int
	Flagged  int
	Done     int
	Missing  int
	Uploaded int
	Swapped  int
	Rejected int
	Errors   int
	Quota    bool
}

func (s Stats) String() string {
	return fmt.Sprintf("manifest=%d flagged=%d already_done=%d missing=%d uploaded=%d swapped=%d rejected=%d errors=%d quota=%t",
		s.Manifest, s.Flagged, s.Done, s.Missing, s.Uploaded, s.Swapped, s.Rejected, s.Errors, s.Quota)
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("--dsn is REQUIRED; refusing to guess the catalog database")
	}
	if opts.Dir == "" {
		return nil, fmt.Errorf("--dir is REQUIRED (the cutout output directory holding manifest.jsonl)")
	}
	if opts.Slot.Column == "" {
		return nil, fmt.Errorf("--slot is REQUIRED; a bare run must not guess which image column it swaps")
	}

	entries, err := readManifest(filepath.Join(opts.Dir, "manifest.jsonl"))
	if err != nil {
		return nil, err
	}
	st := &Stats{Manifest: len(entries)}

	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog: %w", err)
	}
	defer closeDB(db)

	work := make([]entry, 0, len(entries))
	for _, e := range entries {
		if e.Error != "" || len(e.Flagged) > 0 {
			st.Flagged++
			continue
		}
		work = append(work, e)
	}

	pending, err := pendingSources(ctx, db, opts.Slot, work)
	if err != nil {
		return nil, err
	}
	filtered := work[:0]
	for _, e := range work {
		if pending[e.Hash] {
			filtered = append(filtered, e)
			continue
		}
		st.Done++
	}
	work = filtered
	if opts.Limit > 0 && opts.Limit < len(work) {
		work = work[:opts.Limit]
	}

	slog.Info("figure-cutout candidates", "slot", opts.Slot.Name, "manifest", st.Manifest, "flagged", st.Flagged,
		"already_done", st.Done, "to_write", len(work), "apply", opts.Apply)
	if !opts.Apply || len(work) == 0 {
		return st, nil
	}

	r := &runner{db: db, dir: opts.Dir, slot: opts.Slot, gap: opts.UploadGap, stats: st}
	if r.cli, err = newClient(ctx, cfg, opts); err != nil {
		return nil, err
	}
	r.run(ctx, work, opts.Workers)
	if err := r.ping(ctx); err != nil {
		slog.Warn("refping cutout hashes", "err", err)
	}
	slog.Info("figure-cutout done", "result", st.String())
	return st, nil
}

func readManifest(path string) ([]entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()
	var out []entry
	dec := json.NewDecoder(f)
	for dec.More() {
		var e entry
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("decode manifest: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}

// pendingSources answers which of these originals are still the live figure_hash
// of at least one character that has not been swapped yet, so a resumed run
// uploads nothing it would then fail to write.
func pendingSources(ctx context.Context, db *gorm.DB, slot Slot, work []entry) (map[string]bool, error) {
	hashes := make([]string, 0, len(work))
	for _, e := range work {
		hashes = append(hashes, e.Hash)
	}
	out := map[string]bool{}
	for i := 0; i < len(hashes); i += 1000 {
		batch := hashes[i:min(i+1000, len(hashes))]
		var found []string
		err := db.WithContext(ctx).Raw(fmt.Sprintf(
			`SELECT DISTINCT %[1]s FROM catalog_character
			 WHERE %[1]s IN ? AND %[2]s IS NULL AND deleted_at IS NULL`, slot.Column, slot.SourceCol)+" AND "+editspec.NotHumanFieldSQL("field_provenance", slot.Column),
			batch).Scan(&found).Error
		if err != nil {
			return nil, fmt.Errorf("select pending figures: %w", err)
		}
		for _, h := range found {
			out[h] = true
		}
	}
	return out, nil
}

type runner struct {
	db    *gorm.DB
	cli   *imageclient.Client
	dir   string
	slot  Slot
	gap   time.Duration
	stats *Stats

	mu    sync.Mutex
	pings []string
}

type result struct {
	missing, uploaded, swapped, rejected, errors int
	quota                                        bool
	hashes                                       []string
}

func (r *runner) one(ctx context.Context, e entry) result {
	var out result
	path := filepath.Join(r.dir, e.File)
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 {
		out.missing++
		return out
	}

	res, err := r.upload(ctx, body, e.File)
	if err != nil {
		switch {
		case stderrors.Is(err, imageclient.ErrQuotaExceeded):
			slog.Warn("daily image quota exhausted — stopping", "source", e.Hash)
			out.quota = true
		case stderrors.Is(err, imageclient.ErrModerationRejected):
			out.rejected++
			slog.Warn("cutout rejected by moderation", "source", e.Hash)
		default:
			out.errors++
			slog.Warn("upload cutout", "source", e.Hash, "err", err)
		}
		return out
	}
	out.uploaded++
	if res.Hash == e.Hash {
		out.errors++
		slog.Warn("cutout is byte-identical to its source — not swapping", "source", e.Hash)
		return out
	}

	tx := r.db.WithContext(ctx).Exec(fmt.Sprintf(
		`UPDATE catalog_character
		 SET %[2]s = %[1]s, %[1]s = ?, updated_at = now()
		 WHERE %[1]s = ? AND %[2]s IS NULL AND deleted_at IS NULL`, r.slot.Column, r.slot.SourceCol)+" AND "+editspec.NotHumanFieldSQL("field_provenance", r.slot.Column),
		res.Hash, e.Hash)
	if tx.Error != nil {
		out.errors++
		slog.Warn("swap figure hash", "source", e.Hash, "err", tx.Error)
		return out
	}
	out.swapped = int(tx.RowsAffected)
	out.hashes = []string{res.Hash, e.Hash}
	return out
}

func (r *runner) upload(ctx context.Context, body []byte, filename string) (*imageclient.UploadResult, error) {
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

func (r *runner) run(ctx context.Context, work []entry, workers int) {
	if workers < 1 {
		workers = 1
	}
	if workers > len(work) {
		workers = len(work)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	in := make(chan entry)
	out := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range in {
				out <- r.one(ctx, e)
			}
		}()
	}
	go func() {
		defer close(in)
		for _, e := range work {
			select {
			case <-ctx.Done():
				return
			case in <- e:
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
			slog.Info("figure-cutout progress", "done", done, "of", len(work),
				"swapped", r.stats.Swapped, "errors", r.stats.Errors)
		}
	}
}

func (r *runner) absorb(res result) {
	s := r.stats
	s.Missing += res.missing
	s.Uploaded += res.uploaded
	s.Swapped += res.swapped
	s.Rejected += res.rejected
	s.Errors += res.errors
	if res.quota {
		s.Quota = true
	}
	r.mu.Lock()
	r.pings = append(r.pings, res.hashes...)
	r.mu.Unlock()
}

func (r *runner) ping(ctx context.Context) error {
	if r.cli == nil || len(r.pings) == 0 {
		return nil
	}
	for i := 0; i < len(r.pings); i += 1000 {
		batch := r.pings[i:min(i+1000, len(r.pings))]
		if _, err := r.cli.ReferencePing(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func newClient(ctx context.Context, cfg *config.Config, opts Opts) (*imageclient.Client, error) {
	clientCfg := cfg.CatalogImageClient
	if clientCfg.ClientID == "" || clientCfg.ClientSecret == "" {
		return nil, fmt.Errorf("catalog image client credentials are not configured")
	}
	base := opts.ImageBase
	if base == "" {
		base = clientCfg.BaseURL
	}
	if base == "" {
		base = fmt.Sprintf("http://%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
	}
	cli := imageclient.New(imageclient.Config{
		BaseURL: base, CDNBase: cfg.ImageService.CDNBase,
		ClientID: clientCfg.ClientID, ClientSecret: clientCfg.ClientSecret,
		Timeout: clientTimeout,
	})
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := cli.Health(hctx); err != nil {
		return nil, fmt.Errorf("image_service unreachable at %s: %w", base, err)
	}
	return cli, nil
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
