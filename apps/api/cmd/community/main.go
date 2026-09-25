package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"api/internal/app"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	"api/internal/platform/accountpurge"
	authRepo "api/internal/platform/auth/repository"
	commHandler "api/internal/platform/community/handler"
	"api/internal/platform/community/service"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/config"
	"api/pkg/health"
	"api/pkg/logger"
	"api/pkg/trustclient"

	"github.com/gofiber/fiber/v3"
)

const outboxInterval = 60 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	health.MaybeProbe(cfg.CommunityService.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{Name: "kun-community"})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings.NewDistributor(application.DB.DB(), keys.Live(), nil).Start(ctx)

	communityDB, err := database.NewPostgresDB(cfg.CommunityDatabase)
	if err != nil {
		slog.Error("community db connect", "error", err)
		os.Exit(1)
	}

	trustCli := trustclient.New(trustclient.Config{
		BaseURL: cfg.TrustClient.BaseURL, ClientID: cfg.TrustClient.ClientID, ClientSecret: cfg.TrustClient.ClientSecret,
	})

	var forwarder service.Forwarder
	if trustCli != nil {
		forwarder = trustCli
	}
	forwardSvc := service.NewForwardService(communityDB.DB(), forwarder)
	if forwardSvc.Enabled() {
		slog.Info("community trust forwarding enabled", "base_url", cfg.TrustClient.BaseURL)
	} else {
		slog.Info("community trust forwarding disabled (KUN_TRUST_CLIENT_* unset)")
	}

	var scanner service.Scanner
	if trustCli != nil {
		scanner = trustCli
	}
	scanSvc := service.NewScanService(communityDB.DB(), scanner)
	if scanSvc.Enabled() {
		slog.Info("community trust scanning enabled", "base_url", cfg.TrustClient.BaseURL)
	} else {
		slog.Info("community trust scanning disabled (trust.scan_enabled off or KUN_TRUST_CLIENT_* unset)")
	}

	var checker service.Checker
	if trustCli != nil {
		checker = trustCli
	}
	checkSvc := service.NewCheckService(checker)
	if checkSvc.Enabled() {
		slog.Info("community trust check enabled (synchronous word-list gate)", "base_url", cfg.TrustClient.BaseURL)
	} else {
		slog.Info("community trust check disabled (trust.check_enabled off or KUN_TRUST_CLIENT_* unset)")
	}

	sink := service.NewScanningSink(service.NewForwardingSink(service.NoopSink{}, forwardSvc), scanSvc)
	threadSvc := service.NewThreadService(communityDB.DB(), sink, service.WithThreadChecker(checkSvc))
	postSvc := service.NewPostService(communityDB.DB(), sink, service.WithPostChecker(checkSvc))
	reactionSvc := service.NewReactionService(communityDB.DB())
	feedbackSvc := service.NewFeedbackService(communityDB.DB(), sink)
	flagSvc := service.NewFlagService(communityDB.DB(), sink)
	trustSvc := service.NewTrustService(communityDB.DB())
	reviewSvc := service.NewReviewService(communityDB.DB(), sink)
	callbackSvc := service.NewCallbackService(communityDB.DB())
	engagementSvc := service.NewEngagementService(communityDB.DB())
	searchSvc := service.NewSearchService(communityDB.DB())
	boardSvc := service.NewBoardService(communityDB.DB())
	notifySvc := service.NewNotificationService(communityDB.DB())
	followSvc := service.NewFollowService(communityDB.DB())

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())
	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))

	application.Fiber.Post("/trust/callback", commHandler.TrustCallback(cfg.TrustCallbackSecret, callbackSvc))

	clientRepo := siteRepo.NewOAuthClientRepository(application.DB.DB())
	application.Fiber.Use("/api/v1/community", commHandler.S2SAuth(clientRepo))

	api := commHandler.Setup(application.Fiber, commHandler.Services{
		Threads: threadSvc, Posts: postSvc, Reactions: reactionSvc, Feedback: feedbackSvc,
		Flags: flagSvc, Trust: trustSvc, Review: reviewSvc, Engagement: engagementSvc, Search: searchSvc,
		Boards: boardSvc, Notify: notifySvc, Follows: followSvc,
	})

	go notifySvc.Run(ctx)
	slog.Info("community notification dispatcher started")
	go runOutboxTicker(ctx, forwardSvc)
	go runHourlyPrunes(ctx, postSvc)
	accountpurge.Start(ctx, &accountpurge.Consumer{
		Name: "community", Feed: authRepo.NewUserRepository(application.DB.DB()), DB: communityDB.DB(),
		Purge: postSvc.PurgeAccount,
	})

	application.Fiber.Get("/openapi.json", func(c fiber.Ctx) error {
		b, err := json.Marshal(api.OpenAPI())
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/json")
		return c.Send(b)
	})

	slog.Info("community service starting",
		"addr", fmt.Sprintf("%s:%d", cfg.CommunityService.Host, cfg.CommunityService.Port),
		"dbname", cfg.CommunityDatabase.DBName,
	)

	defer func() {
		if err := communityDB.Close(); err != nil {
			slog.Error("close community db", "error", err)
		}
	}()

	if err := application.Run(cfg.CommunityService.Host, cfg.CommunityService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}

func runOutboxTicker(ctx context.Context, fwd *service.ForwardService) {
	t := time.NewTicker(outboxInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := fwd.Sweep(ctx); err != nil {
				slog.Error("community trust-forward sweep", "err", err)
			} else if n > 0 {
				slog.Info("community trust-forward sweep", "forwarded", n)
			}
		}
	}
}

func runHourlyPrunes(ctx context.Context, posts *service.PostService) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := posts.PrunePurgeArchive(ctx); err != nil {
				slog.Error("community purge archive prune", "err", err)
			} else if n > 0 {
				slog.Info("community purge archive prune", "rows", n)
			}
			if n, err := posts.PruneWriteKeys(ctx); err != nil {
				slog.Error("community write key prune", "err", err)
			} else if n > 0 {
				slog.Info("community write key prune", "rows", n)
			}
		}
	}
}
