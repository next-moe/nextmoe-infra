// Command import-chat copies the old kungal and moyu direct messages into
// kun_chat. Both sources go in one run so that a pair who talked on both
// sites gets one conversation with the two histories interleaved in time.
// Re-running imports only what the old sites gained since (the ledger is
// chat_import_message), which is how the window at each site's cutover is
// swept. Group rooms are left for the groups phase.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/chat/service"
	"api/pkg/config"
	"api/pkg/imageclient"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	var (
		sources   = flag.String("sources", "kungal,moyu", "comma-separated: kungal, moyu")
		kungalDB  = flag.String("kungal-db", "kungalgame", "kungal forum database")
		moyuDB    = flag.String("moyu-db", "kungalgame_patch", "moyu database")
		apply     = flag.Bool("apply", false, "actually write; default is a dry run that only reports")
		chatDSN   = flag.String("chat-dsn", "", "explicit kun_chat DSN (tests)")
		mainDSN   = flag.String("main-dsn", "", "explicit kun_galgame_infra DSN (tests)")
		kungalDSN = flag.String("kungal-dsn", "", "explicit kungal DSN (tests)")
		moyuDSN   = flag.String("moyu-dsn", "", "explicit moyu DSN (tests)")
	)
	flag.Parse()
	want := strings.Split(*sources, ",")

	cfg, err := config.Load()
	if err != nil && (*chatDSN == "" || *mainDSN == "") {
		fatal("config", err)
	}
	if cfg != nil {
		logger.Init(cfg.Server.Env)
	}
	open := func(dsn string, base config.DatabaseConfig, name string) *gorm.DB {
		if dsn != "" {
			db, err := database.OpenJob(dsn)
			if err != nil {
				fatal("open "+name, err)
			}
			return db
		}
		base.DBName = name
		conn, err := database.NewPostgresDB(base)
		if err != nil {
			fatal("open "+name, err)
		}
		return conn.DB()
	}
	var dbCfg, chatCfg config.DatabaseConfig
	if cfg != nil {
		dbCfg, chatCfg = cfg.Database, cfg.ChatDatabase
	}
	chat := open(*chatDSN, chatCfg, chatCfg.DBName)
	mainDB := open(*mainDSN, dbCfg, dbCfg.DBName)

	ctx := context.Background()
	d := deps{Chat: chat, Main: mainDB, Apply: *apply}
	if slices.Contains(want, "kungal") {
		d.Kungal = open(*kungalDSN, dbCfg, *kungalDB)
	}
	if slices.Contains(want, "moyu") {
		d.Moyu = open(*moyuDSN, dbCfg, *moyuDB)
	}
	d.Rehost = func(refs map[imageRef]bool) (resolvedImages, int) {
		ic := cfg.ChatImageClient
		if ic.ClientID == "" || ic.ClientSecret == "" {
			fatal("images", fmt.Errorf("KUN_CHAT_IMAGE_CLIENT_* must be set to re-host %d images", len(refs)))
		}
		cli := imageclient.New(imageclient.Config{BaseURL: ic.BaseURL, CDNBase: cfg.ImageService.CDNBase, ClientID: ic.ClientID, ClientSecret: ic.ClientSecret})
		return rehost(ctx, cli, refs)
	}
	rep, err := run(ctx, d)
	if err != nil {
		fatal("import-chat", err)
	}
	if rep.Failed > 0 {
		os.Exit(1)
	}
}

type deps struct {
	Chat, Main, Kungal, Moyu *gorm.DB
	Apply                    bool
	Rehost                   func(map[imageRef]bool) (resolvedImages, int)
}

type report struct {
	Stats                                      stats
	Images, ImagesFailed                       int
	Created, Imported, Skipped, Native, Failed int
}

func run(ctx context.Context, d deps) (*report, error) {
	var srcs []*legacySource
	if d.Kungal != nil {
		src, err := readKungal(ctx, d.Kungal)
		if err != nil {
			return nil, fmt.Errorf("read kungal: %w", err)
		}
		srcs = append(srcs, src)
	}
	if d.Moyu != nil {
		src, err := readMoyu(ctx, d.Moyu)
		if err != nil {
			return nil, fmt.Errorf("read moyu: %w", err)
		}
		srcs = append(srcs, src)
	}
	deleted, err := deletedUsers(ctx, d.Main, srcs)
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	collect := &collectingImages{refs: map[imageRef]bool{}}
	rep := &report{Stats: build(srcs, deleted, collect).Stats, Images: len(collect.refs)}
	slog.Info("import-chat plan", "stats", fmt.Sprintf("%+v", rep.Stats), "images_to_resolve", rep.Images)
	if !d.Apply {
		slog.Info("dry run: nothing was written; re-run with -apply")
		return rep, nil
	}
	resolved := resolvedImages{}
	if len(collect.refs) > 0 {
		resolved, rep.ImagesFailed = d.Rehost(collect.refs)
	}
	final := build(srcs, deleted, resolved)
	rep.Stats = final.Stats
	svc := service.New(d.Chat, service.Options{})
	for _, c := range final.Conversations {
		res, err := svc.Import(ctx, c)
		switch {
		case err == nil:
			rep.Imported += res.Imported
			rep.Skipped += res.Skipped
			if res.Created {
				rep.Created++
			}
		case errors.Is(err, service.ErrNativeHistory):
			rep.Native++
			slog.Warn("pair already talks in chat; history left out", "users", fmt.Sprintf("%d,%d", c.UserA, c.UserB))
		default:
			rep.Failed++
			slog.Error("import pair", "users", fmt.Sprintf("%d,%d", c.UserA, c.UserB), "err", err)
		}
	}
	slog.Info("import-chat done", "report", fmt.Sprintf("%+v", *rep))
	return rep, nil
}

func deletedUsers(ctx context.Context, db *gorm.DB, srcs []*legacySource) (map[int64]bool, error) {
	seen := map[int64]bool{}
	var ids []int64
	for _, s := range srcs {
		for _, r := range s.Rooms {
			for _, u := range r.Users {
				if !seen[u] {
					seen[u] = true
					ids = append(ids, u)
				}
			}
		}
	}
	gone := map[int64]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	for start := 0; start < len(ids); start += 5000 {
		end := min(start+5000, len(ids))
		var live []int64
		if err := db.WithContext(ctx).Raw(`SELECT id FROM users WHERE id IN ? AND anonymized_at IS NULL AND deleted_at IS NULL`, ids[start:end]).
			Scan(&live).Error; err != nil {
			return nil, err
		}
		for _, id := range live {
			delete(gone, id)
		}
	}
	return gone, nil
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
