// Command import-follows copies one source site's follows into
// community_user_follow. It runs once per source at that site's cutover and
// again right after the site ships to sweep the window (inserting follows
// made meanwhile, and with -prune-stale removing follows undone meanwhile).
//
// It never imports kun_galgame_infra.user_follows: that table is a 2026-06-05
// snapshot of moyu (production: 2,648 rows, all written 01:49:37–01:49:50 UTC,
// no code reads or writes it; locally 2,565 of them are in moyu and the other
// 83 are follows moyu users removed since, so importing it would resurrect
// 83 unfollows). kungal's user_follow has 0 rows in production and no
// feature, so it is not a source either. Sources are exactly moyu and letmoe.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	var (
		source       = flag.String("source", "", "moyu or letmoe")
		sourceDB     = flag.String("source-db", "", "source database name (default kungalgame_patch for moyu, kun_letmoe for letmoe)")
		communityDSN = flag.String("community-dsn", "", "explicit community DSN; skips config when both DSNs are given")
		sourceDSN    = flag.String("source-dsn", "", "explicit source DSN")
		apply        = flag.Bool("apply", false, "actually write; default is a dry run that only reports")
		pruneStale   = flag.Bool("prune-stale", false, "with -apply, delete imported edges of this origin that are no longer in the source")
		batch        = flag.Int("batch", 1000, "rows per insert batch")
	)
	flag.Parse()

	if *source != "moyu" && *source != "letmoe" {
		fatal("source", fmt.Errorf("must be moyu or letmoe"))
	}
	if *sourceDB == "" {
		if *source == "moyu" {
			*sourceDB = "kungalgame_patch"
		} else {
			*sourceDB = "kun_letmoe"
		}
	}
	if *batch < 1 {
		*batch = 1
	}

	community, src, closers, err := open(*communityDSN, *sourceDSN, *sourceDB)
	if err != nil {
		fatal("open databases", err)
	}
	defer func() {
		for _, c := range closers {
			_ = c()
		}
	}()

	rep, err := run(context.Background(), community, src, options{
		Source:     *source,
		Apply:      *apply,
		PruneStale: *pruneStale,
		Batch:      *batch,
	})
	if err != nil {
		fatal("import-follows", err)
	}
	rep.log()
	if !rep.Applied {
		slog.Info("dry run: nothing was written; re-run with -apply")
	}
}

func open(communityDSN, sourceDSN, sourceDB string) (community, source *gorm.DB, closers []func() error, err error) {
	if communityDSN != "" && sourceDSN != "" {
		for _, dsn := range []string{communityDSN, sourceDSN} {
			db, oerr := database.OpenJob(dsn)
			if oerr != nil {
				return nil, nil, closers, oerr
			}
			if community == nil {
				community = db
			} else {
				source = db
			}
		}
		return community, source, closers, nil
	}

	cfg, cerr := config.Load()
	if cerr != nil {
		return nil, nil, closers, cerr
	}
	logger.Init(cfg.Server.Env)
	for _, name := range []string{cfg.CommunityDatabase.DBName, sourceDB} {
		dbCfg := cfg.CommunityDatabase
		dbCfg.DBName = name
		conn, oerr := database.NewPostgresDB(dbCfg)
		if oerr != nil {
			return nil, nil, closers, oerr
		}
		closers = append(closers, conn.Close)
		if community == nil {
			community = conn.DB()
		} else {
			source = conn.DB()
		}
	}
	return community, source, closers, nil
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
