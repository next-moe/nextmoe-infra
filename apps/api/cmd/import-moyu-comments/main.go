// Imports moyu's two comment walls into the community primitive.
//
// moyu (kungalgame_patch) kept its comments in patch_comment while the forum,
// letmoe and the sticker site had already moved. The walls are two: a game page
// (patch_comment.resource_id IS NULL) and one patch resource.
//
// The tenant is `moyu`, not `kungal`. moyu's oauth client files CATALOG claims
// as kungal and cannot stop doing so, and the community service used to read
// that same column as its tenant -- which put moyu's walls in the forum's
// tenant, where the anchor id was the only thing keeping the two sites apart
// (1,992 of moyu's 2,040 commented page ids were already forum anchors, 490 of
// them carrying forum posts). oauth_clients.community_site separates them, so
// the anchor id here is moyu's own bare id and nothing else needs a prefix.
package main

import (
	"flag"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	sourceDSN := flag.String("source-dsn", "", "moyu (kungalgame_patch) source DSN; env fallback MOYU_SOURCE_DSN")
	targetDSN := flag.String("target-dsn", "", "kun_community target DSN; env fallback KUN_COMMUNITY_DSN, then KUN_COMMUNITY_* config")
	site := flag.String("site", "moyu", "community site/tenant for the imported threads")
	apply := flag.Bool("apply", false, "perform the writes; default is a dry-run report only")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	srcDSN := firstNonEmpty(*sourceDSN, os.Getenv("MOYU_SOURCE_DSN"))
	if srcDSN == "" {
		slog.Error("--source-dsn (or MOYU_SOURCE_DSN) is required")
		flag.Usage()
		os.Exit(1)
	}
	tgtDSN := firstNonEmpty(*targetDSN, os.Getenv("KUN_COMMUNITY_DSN"), cfg.CommunityDatabase.DSN())

	src, err := openDB(srcDSN)
	if err != nil {
		slog.Error("failed to connect to source (moyu) database", "error", err)
		os.Exit(1)
	}
	tgt, err := openDB(tgtDSN)
	if err != nil {
		slog.Error("failed to connect to target (community) database", "error", err)
		os.Exit(1)
	}

	slog.Info("connected", "source", "patch_comment", "target_db", cfg.CommunityDatabase.DBName,
		"site", *site, "apply", *apply)
	if !*apply {
		slog.Info("DRY RUN — no changes will be written (pass --apply to import)")
	}

	rep, err := run(src, tgt, *site, *apply)
	if err != nil {
		slog.Error("import failed", "error", err)
		os.Exit(1)
	}
	rep.print(os.Stdout, *apply)
}

func openDB(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
