package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/ratingmodel"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "store the fitted model (default: fit and report only)")
	backtest := flag.Bool("backtest", false, "also run the ranking backtests")
	seed := flag.Int64("seed", 1, "backtest random seed")
	top := flag.Int("top", 0, "print the top N works by consensus")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")
	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}
	if *dsn == "" {
		slog.Error("fit-rating-model: --dsn is required; refusing to guess")
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("fit-rating-model: open catalog", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()
	obs, err := ratingmodel.LoadAll(ctx, db)
	if err != nil {
		slog.Error("fit-rating-model: load", "error", err)
		os.Exit(1)
	}
	params, rep, err := ratingmodel.Fit(obs)
	if err != nil {
		slog.Error("fit-rating-model: fit", "error", err)
		os.Exit(1)
	}
	fmt.Printf("works=%d fit_works=%d em_iterations=%d\n", rep.Works, rep.FitWorks, rep.EMIterations)
	fmt.Printf("%-14s %7s %8s %8s %8s %26s %8s %7s %7s %7s %7s\n",
		"source", "works", "prior_n", "within", "tau2", "prior_mean@1/10/100/1000", "reliable", "a", "b", "sigma2", "overlap")
	for _, s := range rep.Sources {
		at := s.PriorMeanAt
		fmt.Printf("%-14s %7d %8.3f %8.3f %8.4f %26s %8d %7.3f %7.3f %7.3f %7d\n",
			s.Source, s.Works, s.PriorVotes, s.Within, s.Tau2,
			fmt.Sprintf("%.2f/%.2f/%.2f/%.2f", at["1"], at["10"], at["100"], at["1000"]),
			s.ReliableWorks, s.A, s.B, s.Sigma2, s.FitWorks)
	}
	if *backtest {
		for _, line := range ratingmodel.Backtest(obs, params, *seed) {
			fmt.Println(line)
		}
	}
	if *top > 0 {
		printTop(ctx, db, obs, params, *top)
	}
	if !*apply {
		fmt.Println("\n=== fit-rating-model DRY RUN (pass --apply to store) ===")
		return
	}
	f, err := ratingmodel.Save(ctx, db, params, rep)
	if err != nil {
		slog.Error("fit-rating-model: save", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== fit-rating-model APPLY: stored model %d ===\n", f.ID)
}

func printTop(ctx context.Context, db *gorm.DB, obs []ratingmodel.Observation, p *ratingmodel.Params, n int) {
	byWork := map[int64][]ratingmodel.Observation{}
	for _, o := range obs {
		byWork[o.WorkID] = append(byWork[o.WorkID], o)
	}
	type row struct {
		id int64
		ws ratingmodel.WorkScore
	}
	var rows []row
	for id, w := range byWork {
		if ws := p.Score(w); ws.Consensus != nil {
			rows = append(rows, row{id, ws})
		}
	}
	for _, metric := range []string{"lower", "mean"} {
		key := func(r row) float64 {
			if metric == "lower" {
				return r.ws.Consensus.Lower
			}
			return r.ws.Consensus.Score
		}
		slices.SortFunc(rows, func(a, b row) int { return cmp.Compare(key(b), key(a)) })
		ids := make([]int64, 0, n)
		for _, r := range rows[:min(n, len(rows))] {
			ids = append(ids, r.id)
		}
		names := map[int64]string{}
		var nr []struct {
			ID          int64
			DisplayName string
		}
		db.WithContext(ctx).Raw(`SELECT id, display_name FROM catalog_work WHERE id IN ?`, ids).Scan(&nr)
		for _, r := range nr {
			names[r.ID] = r.DisplayName
		}
		fmt.Printf("\ntop %d by consensus %s\n", n, metric)
		for i, r := range rows[:min(n, len(rows))] {
			c := r.ws.Consensus
			var parts []string
			for _, s := range r.ws.Sources {
				parts = append(parts, fmt.Sprintf("%s=%.4g(%d)", s.Source, s.Native, s.Votes))
			}
			fmt.Printf("%3d %5.1f [%5.1f-%5.1f] p%6.2f src=%d %-36.36s %s\n", i+1, c.Score, c.Lower, c.Upper, c.Percentile, c.Sources, names[r.id], strings.Join(parts, " "))
		}
	}
}
