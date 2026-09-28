package workengines

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

const (
	LaneVNDB = "vndb"
	LaneBgm  = "bgm"
	LaneAll  = "all"
)

type Opts struct {
	Apply bool
	DSN   string
	Lane  string
}

type Stats struct {
	EnginesCreated int
	EnginesLinked  int

	Releases          int
	ReleasesSame      int
	ReleasesHuman     int
	ReleasesFilled    int
	ReleasesChanged   int
	ReleasesCleared   int
	ReleasesWritten   int
	ReleasesLost      int
	UnknownVNDBEngine int

	BgmStated  int
	BgmCovered int
	BgmWorks   int
	BgmAdd     int
	BgmDrop    int
	Unmapped   map[string]int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess the target database")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}()
	return RunWithDB(ctx, db, opts)
}

func RunWithDB(ctx context.Context, db *gorm.DB, opts Opts) (*Stats, error) {
	if opts.Lane == "" {
		opts.Lane = LaneAll
	}
	runVNDBLane := opts.Lane == LaneVNDB || opts.Lane == LaneAll
	runBgmLane := opts.Lane == LaneBgm || opts.Lane == LaneAll
	if !runVNDBLane && !runBgmLane {
		return nil, fmt.Errorf("unknown lane %q (want %s, %s or %s)", opts.Lane, LaneVNDB, LaneBgm, LaneAll)
	}
	vndbSource, err := sourceID(ctx, db, "vndb")
	if err != nil {
		return nil, err
	}
	bgmSource, err := sourceID(ctx, db, "bangumi")
	if err != nil {
		return nil, err
	}
	st := &Stats{Unmapped: map[string]int{}}
	v, err := loadVocab(ctx, db, opts.Apply, vndbSource, st)
	if err != nil {
		return nil, err
	}
	var forecast map[int64]struct{}
	if runVNDBLane {
		covered, err := runVNDB(ctx, db, opts, v, st)
		if err != nil {
			return nil, err
		}
		if !opts.Apply {
			forecast = covered
		}
	}
	if runBgmLane {
		if err := runBgm(ctx, db, opts, v, bgmSource, forecast, st); err != nil {
			return nil, err
		}
	}
	slog.Info("workengines done", "apply", opts.Apply, "lane", opts.Lane,
		"engines_created", st.EnginesCreated, "engines_linked", st.EnginesLinked,
		"releases", st.Releases, "same", st.ReleasesSame, "human", st.ReleasesHuman,
		"filled", st.ReleasesFilled, "changed", st.ReleasesChanged, "cleared", st.ReleasesCleared,
		"written", st.ReleasesWritten, "lost", st.ReleasesLost, "unknown_vndb_engine", st.UnknownVNDBEngine,
		"bgm_stated", st.BgmStated, "bgm_covered", st.BgmCovered, "bgm_works", st.BgmWorks,
		"bgm_add", st.BgmAdd, "bgm_drop", st.BgmDrop, "unmapped_values", len(st.Unmapped))
	return st, nil
}

func (st *Stats) TopUnmapped(n int) []string {
	type kv struct {
		k string
		v int
	}
	all := make([]kv, 0, len(st.Unmapped))
	for k, v := range st.Unmapped {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].k < all[j].k
	})
	if len(all) > n {
		all = all[:n]
	}
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, fmt.Sprintf("%s×%d", e.k, e.v))
	}
	return out
}

func sourceID(ctx context.Context, db *gorm.DB, key string) (int16, error) {
	var id int16
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error; err != nil {
		return 0, fmt.Errorf("resolve source %q: %w", key, err)
	}
	if id == 0 {
		return 0, fmt.Errorf("catalog_source has no %q row — run the seed first", key)
	}
	return id, nil
}
