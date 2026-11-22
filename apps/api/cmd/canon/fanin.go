package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"api/internal/canon/ent"
	"api/internal/canon/fanin"
	"api/pkg/config"
)

func runFanin(client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("fanin", flag.ExitOnError)
	once := fs.Bool("once", false, "drain the pool once and exit")
	every := fs.Duration("every", time.Minute, "poll interval when not -once")
	limit := fs.Int("limit", 200, "outbox rows per page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, token := config.Env("KUN_CANON_POOL_URL", ""), config.Env("KUN_CANON_POOL_TOKEN", "")
	if base == "" || token == "" {
		return errors.New("KUN_CANON_POOL_URL and KUN_CANON_POOL_TOKEN are required")
	}
	f := fanin.New(config.Env("KUN_CANON_POOL_NAME", "ingest"), fanin.NewHTTPPool(base, token), client)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		for {
			n, err := f.RunOnce(ctx, *limit)
			if err != nil {
				return err
			}
			slog.Info("fanin page", "rows", n)
			if n < *limit {
				break
			}
		}
		if *once {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*every):
		}
	}
}
