package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"api/internal/canon/api"
	"api/internal/canon/ent"
	"api/internal/canon/perm"
	"api/internal/canon/xref"
	"api/pkg/config"
	"api/pkg/health"
	"api/pkg/logger"
	"api/pkg/oidctoken"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	port := config.Env("KUN_CANON_PORT", "9288")
	if n, err := strconv.Atoi(port); err == nil {
		health.MaybeProbe(n, "/healthz")
	}
	if err := config.LoadScope("canon"); err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(config.Env("KUN_ENV", "development"))

	db, err := sql.Open("pgx", catalogDSN()+" search_path=canon")
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	if len(os.Args) > 1 {
		commands := map[string]func([]string) error{
			"fanin":    func(a []string) error { return runFanin(client, a) },
			"takedown": func(a []string) error { return runTakedown(context.Background(), client, a) },
			"restore":  func(a []string) error { return runRestore(context.Background(), client, a) },
			"merge":    func(a []string) error { return runMerge(context.Background(), client, a) },
			"unmerge":  func(a []string) error { return runUnmerge(context.Background(), client, a) },
			"xref-import": func([]string) error {
				n, err := xref.ImportLegacy(context.Background(), db)
				slog.Info("xref import", "rows", n)
				return err
			},
		}
		run, ok := commands[os.Args[1]]
		if !ok {
			slog.Error("unknown command; want fanin, takedown, restore, merge, unmerge or xref-import", "command", os.Args[1])
			os.Exit(2)
		}
		if err := run(os.Args[2:]); err != nil {
			slog.Error(os.Args[1], "error", err)
			os.Exit(1)
		}
		return
	}
	verifier := oidctoken.NewVerifierWithJWKS(config.Env("JWT_SECRET", ""), config.Env("KUN_OIDC_JWKS_URL", "")).
		RequiringIssuer(config.Env("KUN_SITE_URL", ""))
	handler, _ := api.New(client, api.Options{Verifier: verifier})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	idp, err := sql.Open("pgx", idpDSN())
	if err != nil {
		slog.Error("open the IdP database", "error", err)
		os.Exit(1)
	}
	defer idp.Close()
	go perm.Poll(ctx, idp, 30*time.Second)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("canon listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve", "error", err)
		os.Exit(1)
	}
}
