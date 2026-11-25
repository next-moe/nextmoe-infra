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
	"api/internal/canon/safety"
	"api/pkg/config"
	"api/pkg/llmask"
)

func judgeTitles(ctx context.Context, client *ent.Client, limit int) error {
	key := config.Env("KUN_LLM_GATEWAY_KEY", "")
	if key == "" {
		return nil
	}
	llm := llmask.New(config.Env("KUN_LLM_GATEWAY_URL", "http://127.0.0.1:9284/v1"), key, config.Env("KUN_CANON_TITLE_SAFETY_SCENE", "catalog.text-safety.title"))
	st, err := safety.Run(ctx, client, llm, limit)
	if st.Judged+st.Written+st.Failed > 0 {
		slog.Info("title safety", "judged", st.Judged, "written", st.Written, "failed", st.Failed)
	}
	return err
}

func runFanin(client *ent.Client, args []string) error {
	fs := flag.NewFlagSet("fanin", flag.ExitOnError)
	once := fs.Bool("once", false, "drain the pool once and exit")
	every := fs.Duration("every", time.Minute, "poll interval when not -once")
	limit := fs.Int("limit", 200, "outbox rows per page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	base, token := config.Env("KUN_CANON_POOL_URL", ""), config.Env("KUN_CANON_POOL_TOKEN", "")
	// Canon goes live before there is a pool; until one is set the process
	// only judges titles instead of restarting in a loop.
	pooled := base != "" && token != ""
	if !pooled && *once {
		return errors.New("KUN_CANON_POOL_URL and KUN_CANON_POOL_TOKEN are required")
	}
	if !pooled {
		slog.Warn("fanin: no pool is configured (KUN_CANON_POOL_URL, KUN_CANON_POOL_TOKEN)")
	}
	f := fanin.New(config.Env("KUN_CANON_POOL_NAME", "ingest"), fanin.NewHTTPPool(base, token), client)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		for pooled {
			n, err := f.RunOnce(ctx, *limit)
			if err != nil {
				return err
			}
			slog.Info("fanin page", "rows", n)
			if n < *limit {
				break
			}
		}
		// Judged after each drain; a gateway that is down leaves new works
		// unjudged, which is not safe, and never stops the fanin.
		if err := judgeTitles(ctx, client, 400); err != nil {
			slog.Warn("title safety", "error", err)
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
