package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	"api/internal/jobs/ymgalnews"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	lanes := flag.String("lanes", "news,column", "which upstream lanes to ingest: news, column, or both")
	pages := flag.Int("pages", 1, "pages to walk per lane (10 items per page); stops early on an empty page or a full page of known unchanged rows")
	apply := flag.Bool("apply", false, "write to kun_news and publish the source's pending rows, per the 2026-09-29 standing release (default: dry-run forecast only)")
	gap := flag.Duration("gap", time.Second, "delay between page requests; raise if the run reports rate_limited > 0")
	noImages := flag.Bool("no-images", false, "skip banner download/upload entirely (text rows only)")
	markDead := flag.Bool("mark-dead", false, "actually hide rows the scan proves are gone upstream (default: report only)")
	dsn := flag.String("dsn", "", "kun_news DSN override (default: KUN_NEWS_PG_*)")
	imageBase := flag.String("image-base", "", "image_service base URL override (local dev)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	var laneList []string
	for _, l := range strings.Split(*lanes, ",") {
		if l = strings.TrimSpace(l); l != "" {
			laneList = append(laneList, l)
		}
	}

	summary, err := ymgalnews.Run(context.Background(), cfg, ymgalnews.Opts{
		Lanes:     laneList,
		Pages:     *pages,
		Apply:     *apply,
		Gap:       *gap,
		NoImages:  *noImages,
		MarkDead:  *markDead,
		DSN:       *dsn,
		ImageBase: *imageBase,
	})
	if summary != nil {
		b, _ := json.MarshalIndent(summary, "", "  ")
		os.Stdout.Write(append(b, '\n'))
	}
	if err != nil {
		slog.Error("import-ymgal-news", "error", err)
		os.Exit(1)
	}
	if n, _ := summary["images_failed"].(int); n > 0 {
		os.Exit(1)
	}
}
