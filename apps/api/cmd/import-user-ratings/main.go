package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/jobs/ratingimport"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	forumDSN := flag.String("forum-dsn", "", "kungalgame DSN; empty leaves the forum out")
	forumClient := flag.String("forum-client", "", "the forum's OAuth client id, recorded as each forum rating's origin (required with -forum-dsn)")
	letmoeDSN := flag.String("letmoe-dsn", "", "kun_letmoe DSN; empty leaves LetMoe out")
	letmoeClient := flag.String("letmoe-client", "", "LetMoe's OAuth client id, recorded as each LetMoe rating's origin (required with -letmoe-dsn)")
	apply := flag.Bool("apply", false, "write changes (default dry: everything runs and rolls back)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")
	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}
	if *dsn == "" || (*forumDSN == "" && *letmoeDSN == "") ||
		(*forumDSN != "" && *forumClient == "") || (*letmoeDSN != "" && *letmoeClient == "") {
		slog.Error("import-user-ratings: need -dsn, at least one source DSN, and the client id of each source given; refusing to guess")
		os.Exit(2)
	}
	open := func(name, d string) *gorm.DB {
		db, err := database.OpenJob(d)
		if err != nil {
			slog.Error("import-user-ratings: open", "db", name, "error", err)
			os.Exit(1)
		}
		return db
	}
	var sources []ratingimport.Source
	if *forumDSN != "" {
		sources = append(sources, ratingimport.Source{Site: ratingimport.SiteKungal, ClientID: *forumClient, DB: open("forum", *forumDSN)})
	}
	if *letmoeDSN != "" {
		sources = append(sources, ratingimport.Source{Site: ratingimport.SiteLetmoe, ClientID: *letmoeClient, DB: open("letmoe", *letmoeDSN)})
	}
	res, err := ratingimport.Run(context.Background(), ratingimport.Opts{
		Catalog: open("catalog", *dsn), Sources: sources, Apply: *apply,
	})
	if err != nil {
		slog.Error("import-user-ratings failed", "error", err)
		os.Exit(1)
	}
	mode := "DRY RUN"
	if *apply {
		mode = "APPLY"
	}
	fmt.Printf("\n=== import-user-ratings %s ===\n", mode)
	for _, s := range res.Sites {
		fmt.Printf("%s: read=%d inserted=%d updated=%d unchanged=%d adopted=%d skipped_gone=%d skipped_work=%d redirected=%d invalid=%d withdrawn=%d states_seeded=%d states_kept=%d\n",
			s.Site, s.Read, s.Inserted, s.Updated, s.Unchanged, s.Adopted, s.SkippedGone, s.SkippedWork,
			s.Redirected, s.Invalid, s.Withdrawn, s.StatesSeeded, s.StatesKept)
	}
	fmt.Printf("aggregates: rated=%d changed=%d deleted=%d\n", res.Aggregates.Rated, res.Aggregates.Written, res.Aggregates.Deleted)
}
