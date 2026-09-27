package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"api/internal/app"
	"api/internal/infrastructure/cache"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	"api/internal/platform/accountpurge"
	authRepo "api/internal/platform/auth/repository"
	chatHandler "api/internal/platform/chat/handler"
	"api/internal/platform/chat/realtime"
	"api/internal/platform/chat/service"
	"api/internal/platform/chat/source"
	"api/internal/platform/devapi"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/config"
	"api/pkg/health"
	"api/pkg/imageclient"
	"api/pkg/logger"
	"api/pkg/oidctoken"
	"api/pkg/trustclient"

	"github.com/gofiber/fiber/v3"
)

const (
	realtimeTokenTTL = 15 * time.Minute
	forwardInterval  = time.Minute
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	health.MaybeProbe(cfg.ChatService.Port, "/healthz")
	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{Name: "kun-chat"})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chatDB, err := database.NewPostgresDB(cfg.ChatDatabase)
	if err != nil {
		slog.Error("chat db connect", "error", err)
		os.Exit(1)
	}
	communityDB, err := database.NewPostgresDB(cfg.CommunityDatabase)
	if err != nil {
		slog.Error("community db connect", "error", err)
		os.Exit(1)
	}
	mainDB := application.DB.DB()

	users := source.NewUsers(mainDB)
	opts := service.Options{
		Users:         users,
		Relationships: source.NewRelationships(communityDB.DB()),
		ImageBaseURL:  cfg.ImageService.CDNBase,
	}
	if rc, err := cache.NewRedisCache(cfg.Redis); err != nil {
		slog.Warn("chat: redis unavailable; rate limits are off", "err", err)
	} else {
		opts.Counter = devapi.NewRedisStore(rc)
	}
	if ic := cfg.ChatImageClient; ic.ClientID != "" && ic.ClientSecret != "" {
		opts.Images = source.NewImages(imageclient.New(imageclient.Config{
			BaseURL: ic.BaseURL, CDNBase: cfg.ImageService.CDNBase, ClientID: ic.ClientID, ClientSecret: ic.ClientSecret,
		}))
	} else {
		slog.Info("chat: image messages disabled (KUN_CHAT_IMAGE_CLIENT_* unset)")
	}
	if pub := realtime.NewCentrifugo(cfg.ChatService.CentrifugoAPIURL, cfg.ChatService.CentrifugoAPIKey); pub != nil {
		opts.Publisher = pub
		go pub.Run(ctx)
		slog.Info("chat realtime publishing enabled", "api_url", cfg.ChatService.CentrifugoAPIURL)
	} else {
		slog.Info("chat realtime publishing disabled (KUN_CHAT_CENTRIFUGO_* unset); clients poll")
	}
	svc := service.New(chatDB.DB(), opts)

	verifier := oidctoken.NewVerifierWithJWKS(cfg.JWT.Secret, cfg.OIDC.JWKSURL).RequiringIssuer(cfg.OIDC.Issuer)
	clients := siteRepo.NewOAuthClientRepository(mainDB)

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())
	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))

	application.Fiber.Post("/trust/callback", chatHandler.TrustCallback(cfg.TrustCallbackSecret, svc))

	api := chatHandler.Setup(application.Fiber, chatHandler.Options{
		Chat:        svc,
		Tokens:      realtime.NewTokenIssuer(cfg.ChatService.RealtimeTokenSecret, realtimeTokenTTL),
		RealtimeURL: cfg.ChatService.RealtimeURL,
		Identify: func(ctx context.Context, raw string) (chatHandler.Identity, error) {
			claims, err := verifier.Parse(ctx, raw)
			if err != nil {
				return chatHandler.Identity{}, err
			}
			return chatHandler.Identity{UID: int64(claims.ID), ClientID: claims.ClientID, Scopes: strings.Fields(claims.Scope)}, nil
		},
		AccountActive: func(ctx context.Context, uid int64) (bool, error) {
			profiles, err := users.Profiles(ctx, []int64{uid})
			if err != nil {
				return false, err
			}
			p, ok := profiles[uid]
			return ok && !p.Deleted, nil
		},
		Client: func(ctx context.Context, clientID string) (chatHandler.ClientInfo, error) {
			cl, err := clients.FindByClientID(ctx, clientID)
			if err != nil || cl == nil {
				return chatHandler.ClientInfo{}, err
			}
			return chatHandler.ClientInfo{Site: cl.CommunityTenant(), Hosts: redirectHosts(cl.RedirectURIs)}, nil
		},
	})
	spec, err := json.Marshal(api.OpenAPI())
	if err != nil {
		slog.Error("marshal chat spec", "error", err)
		os.Exit(1)
	}
	specETag := fmt.Sprintf(`"%x"`, sha256.Sum256(spec))
	application.Fiber.Get(chatHandler.SpecPath, func(c fiber.Ctx) error {
		// Under max-age=3600 Cloudflare kept serving 1.0.1 after 1.1.0 was deployed
		// and rewrote the browser max-age to 14400; kungal-apps fetched the live
		// spec minutes after the deploy and got the old version.
		c.Set("Cache-Control", "no-cache")
		c.Set("ETag", specETag)
		if c.Get("If-None-Match") == specETag {
			return c.SendStatus(fiber.StatusNotModified)
		}
		c.Set("Content-Type", "application/json")
		return c.Send(spec)
	})

	accountpurge.Start(ctx, &accountpurge.Consumer{
		Name: "chat", Feed: authRepo.NewUserRepository(mainDB), DB: chatDB.DB(), Purge: svc.PurgeAccount,
	})
	go runHourly(ctx, svc)
	if trust := trustclient.New(trustclient.Config{
		BaseURL: cfg.TrustClient.BaseURL, ClientID: cfg.TrustClient.ClientID, ClientSecret: cfg.TrustClient.ClientSecret,
	}); trust != nil {
		go runForwarder(ctx, svc, trust)
		slog.Info("chat report forwarding enabled", "base_url", cfg.TrustClient.BaseURL)
	} else {
		slog.Info("chat report forwarding disabled (KUN_TRUST_CLIENT_* unset); reports wait in chat_report")
	}

	slog.Info("chat service starting",
		"addr", fmt.Sprintf("%s:%d", cfg.ChatService.Host, cfg.ChatService.Port),
		"dbname", cfg.ChatDatabase.DBName,
	)
	defer func() {
		_ = chatDB.Close()
		_ = communityDB.Close()
	}()
	if err := application.Run(cfg.ChatService.Host, cfg.ChatService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}

func redirectHosts(raw []byte) []string {
	var uris []string
	if err := json.Unmarshal(raw, &uris); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var hosts []string
	for _, u := range uris {
		p, err := url.Parse(u)
		if err != nil || p.Scheme != "https" || p.Hostname() == "" || seen[p.Hostname()] {
			continue
		}
		seen[p.Hostname()] = true
		hosts = append(hosts, p.Hostname())
	}
	return hosts
}

func runHourly(ctx context.Context, svc *service.Service) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	var lastPing time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := svc.PruneUpdates(ctx); err != nil {
				slog.Error("chat update prune", "err", err)
			} else if n > 0 {
				slog.Info("chat update prune", "rows", n)
			}
			if time.Since(lastPing) >= 24*time.Hour {
				updated, missing, err := svc.PingImages(ctx)
				if err != nil {
					slog.Error("chat image refping", "err", err)
					continue
				}
				lastPing = time.Now()
				slog.Info("chat image refping", "updated", updated, "not_found", missing)
			}
		}
	}
}

func runForwarder(ctx context.Context, svc *service.Service, fwd service.Forwarder) {
	t := time.NewTicker(forwardInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := svc.ForwardReports(ctx, fwd); err != nil {
				slog.Error("chat report forward", "err", err)
			} else if n > 0 {
				slog.Info("chat report forward", "forwarded", n)
			}
		}
	}
}
