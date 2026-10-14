// Bangumi game credits were written under roles of their own (剧本, 原画, 音乐,
// 导演, 制作总指挥) instead of the roles VNDB, ErogameScape and DLsite share, so
// one creator showed twice on a work, once under 脚本 and again under 剧本. The
// seed now maps those positions onto the shared roles; this moves the credits
// already written. A credit whose twin already exists under the shared role is
// deleted instead of moved. Only bangumi-sourced credits on game works move.
package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/seed"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED, never inferred from the environment")
	apply := flag.Bool("apply", false, "write (default: dry run, counts only)")
	receipts := flag.String("receipts", "", "write one JSON line per credit moved (action move) or deleted as a twin (action delete), with the full row before the change")
	flag.Parse()

	if *dsn == "" {
		slog.Error("repair-bangumi-game-roles", "error", "--dsn is required")
		os.Exit(2)
	}
	generated, err := seed.GenerateBangumiRoles()
	if err != nil {
		slog.Error("repair-bangumi-game-roles: role table", "error", err)
		os.Exit(1)
	}

	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("repair-bangumi-game-roles: connect", "error", err)
		os.Exit(1)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	var out io.Writer = io.Discard
	if *receipts != "" {
		f, err := os.Create(*receipts)
		if err != nil {
			slog.Error("repair-bangumi-game-roles: open receipts", "path", *receipts, "error", err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	st, err := run(context.Background(), db, generated.GameRoleToShared, *apply, out)
	slog.Info("repair-bangumi-game-roles summary", "apply", *apply, "moved", st.Moved, "deleted", st.Deleted, "works", st.Works)
	if err != nil {
		slog.Error("repair-bangumi-game-roles", "error", err)
		os.Exit(1)
	}
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
}
