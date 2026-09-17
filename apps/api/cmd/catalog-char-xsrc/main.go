package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	mode := flag.String("mode", "", "packets | emit | panel-packets | panel-emit | emit-all")
	pairsPath := flag.String("pairs", "pairs.jsonl", "pair metadata JSONL (written by packets, read by emit)")
	packetsPath := flag.String("packets", "packets.jsonl", "evidence packets JSONL for catalog-adjudicate (packets mode)")
	verdictsPath := flag.String("verdicts", "", "verdicts JSONL from catalog-adjudicate (emit mode)")
	worklistPath := flag.String("worklist", "worklist.jsonl", "merge worklist for catalog-dedup-batch (emit mode)")
	reviewPath := flag.String("review", "review.txt", "human-review tail (emit mode)")
	pairs2Path := flag.String("pairs2", "pairs2.jsonl", "panel pair metadata JSONL (written by panel-packets, read by panel-emit)")
	packets2Path := flag.String("packets2", "packets2.jsonl", "panel vote packets JSONL (panel-packets mode)")
	verdicts2Path := flag.String("verdicts2", "", "panel verdicts JSONL from catalog-adjudicate (panel-emit mode)")
	residualPath := flag.String("residual", "residual.txt", "panel residual tail (panel-emit mode)")
	flag.Parse()

	switch *mode {
	case "packets", "panel-packets":
		_ = godotenv.Load("apps/api/.env")
		cfg, err := config.Load()
		if err != nil {
			slog.Error("load config", "error", err)
			os.Exit(1)
		}
		logger.Init(cfg.Server.Env)
		catalogDB, err := database.NewPostgresDB(cfg.CatalogDatabase)
		if err != nil {
			slog.Error("catalog db connect", "error", err)
			os.Exit(1)
		}
		defer catalogDB.Close()
		if *mode == "panel-packets" {
			if *verdictsPath == "" {
				fmt.Fprintln(os.Stderr, "-verdicts (round one) is required in panel-packets mode")
				os.Exit(2)
			}
			if err := runPanelPackets(catalogDB.DB(), *pairsPath, *verdictsPath,
				*pairs2Path, *packets2Path, os.Stdout); err != nil {
				slog.Error("panel-packets", "error", err)
				os.Exit(1)
			}
			return
		}
		if err := runPackets(catalogDB.DB(), *pairsPath, *packetsPath); err != nil {
			slog.Error("packets", "error", err)
			os.Exit(1)
		}
	case "panel-emit":
		logger.Init("development")
		if *verdicts2Path == "" {
			fmt.Fprintln(os.Stderr, "-verdicts2 is required in panel-emit mode")
			os.Exit(2)
		}
		if err := runPanelEmit(*pairs2Path, *verdicts2Path, *worklistPath, *residualPath, os.Stdout); err != nil {
			slog.Error("panel-emit", "error", err)
			os.Exit(1)
		}
	case "emit-all":
		logger.Init("development")
		if *verdictsPath == "" || *verdicts2Path == "" {
			fmt.Fprintln(os.Stderr, "-verdicts and -verdicts2 are required in emit-all mode")
			os.Exit(2)
		}
		if err := runEmitAll(*pairsPath, *verdictsPath, *pairs2Path, *verdicts2Path,
			*worklistPath, *residualPath, os.Stdout); err != nil {
			slog.Error("emit-all", "error", err)
			os.Exit(1)
		}
	case "emit":
		logger.Init("development")
		if *verdictsPath == "" {
			fmt.Fprintln(os.Stderr, "-verdicts is required in emit mode")
			os.Exit(2)
		}
		if err := runEmit(*pairsPath, *verdictsPath, *worklistPath, *reviewPath, os.Stdout); err != nil {
			slog.Error("emit", "error", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown -mode %q (packets | emit | panel-packets | panel-emit | emit-all)\n", *mode)
		os.Exit(2)
	}
}

func runPackets(db *gorm.DB, pairsPath, packetsPath string) error {
	pairs, info, err := buildPairs(db)
	if err != nil {
		return err
	}
	titles, err := loadWorkTitles(db, pairs)
	if err != nil {
		return err
	}

	pf, err := os.Create(pairsPath)
	if err != nil {
		return err
	}
	defer pf.Close()
	kf, err := os.Create(packetsPath)
	if err != nil {
		return err
	}
	defer kf.Close()
	pe, ke := json.NewEncoder(pf), json.NewEncoder(kf)

	tiers := map[int]int{}
	for _, p := range pairs {
		tiers[p.Tier]++
		if err := pe.Encode(p); err != nil {
			return err
		}
		if err := ke.Encode(packetFor(p, info, titles)); err != nil {
			return err
		}
	}
	slog.Info("packets built", "pairs", len(pairs),
		"tier1_fold_equal", tiers[1], "tier2_va_bridge", tiers[2], "tier3_name_similar", tiers[3])
	return nil
}
