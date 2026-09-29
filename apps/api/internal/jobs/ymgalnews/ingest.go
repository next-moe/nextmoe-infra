package ymgalnews

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/news/model"
	"api/internal/platform/news/service"
	"api/pkg/config"

	"gorm.io/gorm"
)

const (
	LaneNews   = model.LaneNews
	LaneColumn = model.LaneColumn
)

// deadCandidateCeiling caps how many rows one run may hide. 苍麟 removes ad spam
// by hand, a few rows at a time; a run that wants to bury more than this has
// almost certainly mis-read the upstream rather than found a purge.
const deadCandidateCeiling = 5

const releaseReason = "ymgal standing release: trusted partner feed, no review (user adjudication 2026-09-29)"

type Opts struct {
	Lanes     []string
	Pages     int
	Apply     bool
	Gap       time.Duration
	NoImages  bool
	MarkDead  bool
	DSN       string
	ImageBase string
}

func DefaultOpts() Opts {
	return Opts{Lanes: []string{LaneNews, LaneColumn}, Pages: 1, Gap: time.Second}
}

type stats struct {
	fetched     int
	created     int
	updated     int
	unchanged   int
	truncated   int
	laneFlip    int
	skipped     int
	imagesUp    int
	imagesFail  int
	wouldCreate int
	wouldUpdate int
	markedDead  int
	released    int
}

func (s *stats) summary(c *Client, lanes []string, deadCandidates []string, opts Opts) map[string]any {
	out := map[string]any{
		"lanes":         lanes,
		"pages":         opts.Pages,
		"apply":         opts.Apply,
		"fetched":       s.fetched,
		"created":       s.created,
		"updated":       s.updated,
		"unchanged":     s.unchanged,
		"truncated":     s.truncated,
		"skipped":       s.skipped,
		"images_up":     s.imagesUp,
		"images_failed": s.imagesFail,
		"rate_limited":  c.RateLimitedCount(),
	}
	if !opts.Apply {
		out["would_create"] = s.wouldCreate
		out["would_update"] = s.wouldUpdate
	}
	if s.laneFlip > 0 {
		// The unique key is (source_key, external_id) on the assumption that news
		// and column share one snowflake id space. A flip means they do not.
		out["lane_flip"] = s.laneFlip
	}
	if len(deadCandidates) > 0 {
		out["would_dead"] = deadCandidates
	}
	if s.markedDead > 0 {
		out["marked_dead"] = s.markedDead
	}
	if opts.Apply {
		out["released"] = s.released
	}
	return out
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (map[string]any, error) {
	if cfg.Ymgal.ClientID == "" || cfg.Ymgal.ClientSecret == "" {
		return nil, fmt.Errorf("ymgal client not configured (set KUN_YMGAL_CLIENT_ID / KUN_YMGAL_CLIENT_SECRET)")
	}
	if len(opts.Lanes) == 0 {
		opts.Lanes = []string{LaneNews, LaneColumn}
	}
	for _, lane := range opts.Lanes {
		if !model.IsKnownLane(lane) {
			return nil, fmt.Errorf("unknown lane %q (want %s or %s)", lane, LaneNews, LaneColumn)
		}
	}
	if opts.Pages < 1 {
		opts.Pages = 1
	}

	db, closeDB, err := openNewsDB(cfg, opts.DSN)
	if err != nil {
		return nil, err
	}
	defer closeDB()

	w, err := newWriter(cfg, db, opts)
	if err != nil {
		return nil, err
	}

	client := NewClient(Config{
		BaseURL:      cfg.Ymgal.BaseURL,
		ClientID:     cfg.Ymgal.ClientID,
		ClientSecret: cfg.Ymgal.ClientSecret,
	})

	st := &stats{}
	// A single fetch error anywhere disqualifies the liveness pass: "we did not
	// see it" is only evidence when the scan was complete.
	complete := true
	oldestSeen := time.Time{}
	seen := map[string]struct{}{}

	for _, lane := range opts.Lanes {
		for page := 1; page <= opts.Pages; page++ {
			topics, err := client.Topics(ctx, lane, page)
			if err != nil {
				complete = false
				slog.Error("ymgal: fetch failed", "lane", lane, "page", page, "err", err)
				break
			}
			if len(topics) == 0 {
				break
			}
			st.fetched += len(topics)

			allKnown := true
			for _, t := range topics {
				outcome, err := w.apply(ctx, lane, t, st)
				if err != nil {
					complete = false
					slog.Error("ymgal: apply failed", "lane", lane, "topic", t.TopicID, "err", err)
					continue
				}
				if outcome != outcomeUnchanged {
					allKnown = false
				}
				seen[t.TopicID] = struct{}{}
				if ts, err := t.PublishedAt(); err == nil {
					if oldestSeen.IsZero() || ts.Before(oldestSeen) {
						oldestSeen = ts
					}
				}
			}

			// Stop on a FULL page of already-known unchanged rows, never on a
			// single hit: the upstream can pin or re-surface an old topic, so one
			// familiar item does not mean the new material has been passed.
			if allKnown && len(topics) == pageSize {
				break
			}
			if opts.Gap > 0 && page < opts.Pages {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(opts.Gap):
				}
			}
		}
	}

	if opts.Apply {
		n, err := service.ReleasePendingImports(ctx, db, model.SourceKeyYmgal, model.SystemActorUID, releaseReason)
		if err != nil {
			return nil, fmt.Errorf("standing release: %w", err)
		}
		st.released = n
	}

	deadCandidates, err := w.livenessCandidates(ctx, complete, oldestSeen, seen)
	if err != nil {
		return nil, err
	}
	if len(deadCandidates) > 0 {
		slog.Warn("ymgal: rows held locally but absent upstream in the scanned window",
			"count", len(deadCandidates), "mark_dead", opts.MarkDead, "ids", deadCandidates)
	}
	if opts.MarkDead && opts.Apply && len(deadCandidates) > 0 {
		if len(deadCandidates) > deadCandidateCeiling {
			return st.summary(client, opts.Lanes, deadCandidates, opts), fmt.Errorf(
				"refusing to hide %d rows in one run (ceiling %d): a purge this large means the scan or the upstream is wrong, not that %d ads were deleted",
				len(deadCandidates), deadCandidateCeiling, len(deadCandidates))
		}
		n, err := w.markDead(ctx, deadCandidates)
		if err != nil {
			return nil, err
		}
		st.markedDead = n
	}

	summary := st.summary(client, opts.Lanes, deadCandidates, opts)
	if client.RateLimitedCount() > 0 {
		slog.Warn("ymgal: upstream rate limited us — this run's counters are the evidence for the steady-state cadence",
			"count", client.RateLimitedCount(), "gap", opts.Gap)
	}
	if !complete {
		return summary, fmt.Errorf("run incomplete: at least one page or row failed (liveness judgement was skipped)")
	}
	return summary, nil
}

func openNewsDB(cfg *config.Config, dsn string) (*gorm.DB, func(), error) {
	if dsn != "" {
		db, err := database.OpenJobWith(dsn, &gorm.Config{})
		if err != nil {
			return nil, nil, fmt.Errorf("news db (--dsn): %w", err)
		}
		return db, func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}, nil
	}
	conn, err := database.NewPostgresDB(cfg.NewsDatabase)
	if err != nil {
		return nil, nil, fmt.Errorf("news db: %w", err)
	}
	return conn.DB(), func() { _ = conn.Close() }, nil
}
