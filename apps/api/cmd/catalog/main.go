package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"api/internal/app"
	"api/internal/galgameapp"
	"api/internal/infrastructure/cache"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	v2handler "api/internal/platform/apiv2/handler"
	"api/internal/platform/apiv2/protocol"
	"api/internal/platform/catalog/editspec"
	catHandler "api/internal/platform/catalog/handler"
	"api/internal/platform/catalog/repository"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/service"
	"api/internal/platform/devapi"
	"api/internal/platform/editing"
	newsHandler "api/internal/platform/news/handler"
	newsService "api/internal/platform/news/service"
	"api/internal/platform/permissions"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteRepo "api/internal/platform/site/repository"
	"api/internal/platform/store/price"
	storeService "api/internal/platform/store/service"
	"api/internal/platform/store/shortener"
	"api/pkg/config"
	"api/pkg/health"
	"api/pkg/imageclient"
	"api/pkg/logger"
	"api/pkg/oidctoken"

	"github.com/gofiber/fiber/v3"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	health.MaybeProbe(cfg.CatalogService.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{Name: "kun-catalog"})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}

	permCtx, cancelPerm := context.WithCancel(context.Background())
	defer cancelPerm()
	settings.NewDistributor(application.DB.DB(), keys.Live(), nil).Start(permCtx)

	catalogDB, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}

	galgameDB, err := database.NewPostgresDB(cfg.GalgameDatabase)
	if err != nil {
		slog.Error("galgame db connect", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := galgameDB.Close(); err != nil {
			slog.Error("close galgame db", "error", err)
		}
	}()

	redirects := repository.NewRedirectRepository(catalogDB.DB())
	resolveSvc := service.NewResolveService(redirects)
	mergeSvc := service.NewMergeService(catalogDB.DB(), resolveSvc,
		repository.NewProposalRepository(catalogDB.DB()), repository.NewRevisionRepository(catalogDB.DB()))
	queueSvc := service.NewAdminQueueService(catalogDB.DB(), mergeSvc)

	readSvc := service.NewReadService(catalogDB.DB())
	statsSvc := service.NewStatsService(catalogDB.DB())
	searcher, engineName, err := catalogSearch.NewIndexerFromConfig(cfg)
	if err != nil {
		slog.Error("search indexer", "error", err)
		os.Exit(1)
	}
	if err := searcher.Health(context.Background()); err != nil {
		slog.Error("opensearch unhealthy", "engine", engineName, "error", err)
		os.Exit(1)
	}

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())
	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))

	clientRepo := siteRepo.NewOAuthClientRepository(application.DB.DB())

	tokenVerifier := oidctoken.NewVerifierWithJWKS(cfg.JWT.Secret, cfg.OIDC.JWKSURL)
	application.Fiber.Use("/api/v1/admin/catalog",
		middleware.JWTAuth(tokenVerifier), catHandler.AdminGate(clientRepo))

	var devCache *cache.RedisCache
	if rc, err := cache.NewRedisCache(cfg.Redis); err != nil {
		slog.Warn("devapi: redis unavailable — rate-limit/quota will fail open", "err", err)
	} else {
		devCache = rc
	}
	devStore := devapi.NewRedisStore(devCache)

	claimSvc := service.NewClaimLifecycleService(catalogDB.DB())
	catHandler.SetupAdmin(application.Fiber, queueSvc, mergeSvc, claimSvc,
		service.NewImageReferenceService(catalogDB.DB()))

	editRegistry := editing.NewRegistry()
	if err := editspec.RegisterAll(editRegistry, catalogDB.DB()); err != nil {
		slog.Error("editing: register catalog entity types", "error", err)
		os.Exit(1)
	}
	editEngine := editing.NewEngine(catalogDB.DB(), editRegistry)
	coverVoteSvc := service.NewCoverVoteService(catalogDB.DB())
	playtimeSvc := service.NewUserPlaytimeService(catalogDB.DB())
	workStateSvc := service.NewUserWorkStateService(catalogDB.DB())
	folderSvc := service.NewUserFolderService(catalogDB.DB())

	// kun_news is a SECOND database on a process whose primary job is catalog.
	// An unreachable news database degrades the news face to 503 instead of
	// exiting: the first production deploy necessarily precedes
	// `CREATE DATABASE kun_news`, and a hard failure there would crash-loop the
	// catalog container over a face nobody is calling yet.
	var newsSvc *newsService.PublicService
	var newsAdminSvc *newsService.AdminService
	var newsWriteSvc *newsService.SubmissionService
	if newsDB, err := database.NewPostgresDB(cfg.NewsDatabase); err != nil {
		slog.Warn("news db connect failed — /v2/news degraded to 503", "dbname", cfg.NewsDatabase.DBName, "error", err)
	} else {
		defer func() {
			if err := newsDB.Close(); err != nil {
				slog.Error("close news db", "error", err)
			}
		}()
		newsSvc = newsService.NewPublicService(newsDB.DB(), cfg.ImageService.CDNBase)
		newsAdminSvc = newsService.NewAdminService(newsDB.DB(), cfg.ImageService.CDNBase)
		newsWriteSvc = newsService.NewSubmissionService(newsDB.DB(), cfg.ImageService.CDNBase)
	}

	// The moderation face is the human half of the gate 月幕 asked for. It is a
	// separate prefix with its own permission rather than a filter on the public
	// face: the public face's whole contract is that unpublished items are not
	// addressable there.
	newsAdminH := newsHandler.NewAdminHandler(newsAdminSvc)
	application.Fiber.Use(newsHandler.AdminPrefix,
		middleware.JWTAuth(tokenVerifier), newsHandler.AdminGate(clientRepo))
	adminNews := application.Fiber.Group(newsHandler.AdminPrefix)
	adminNews.Get("/stats", newsAdminH.Stats)
	adminNews.Get("/items", newsAdminH.Queue)
	adminNews.Get("/items/:id", newsAdminH.Detail)
	adminNews.Post("/items/:id/decision", newsAdminH.Decide)

	setupPublicCatalog(application, cfg, catalogDB, readSvc, resolveSvc, searcher, statsSvc,
		clientRepo, tokenVerifier, devStore, devCache, newsSvc, newsWriteSvc, editRegistry, playtimeSvc, workStateSvc, folderSvc, coverVoteSvc, claimSvc, editEngine)

	galgameapp.MountRetiredPublic(application)
	// Wave R3 (2026-08-27): every v1 face this binary served is gone, so the
	// prefixes answer 410. Mounted here, after the admin groups and after
	// setupPublicCatalog registered /v2 — fiber matches in registration order,
	// and an earlier mount would put the tombstone in front of a live route.
	catHandler.MountRetiredV1(application.Fiber)

	permissions.NewDistributor(application.DB.DB(), permissions.Live(), nil).Start(permCtx)

	slog.Info("catalog service starting",
		"addr", fmt.Sprintf("%s:%d", cfg.CatalogService.Host, cfg.CatalogService.Port),
		"dbname", cfg.CatalogDatabase.DBName,
		"search_engine", engineName,
	)

	defer func() {
		if err := catalogDB.Close(); err != nil {
			slog.Error("close catalog db", "error", err)
		}
	}()

	if err := application.Run(cfg.CatalogService.Host, cfg.CatalogService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}

func setupPublicCatalog(
	application *app.App,
	cfg *config.Config,
	catalogDB *database.PostgresDB,
	readSvc *service.ReadService,
	resolveSvc *service.ResolveService,
	searcher *catalogSearch.Indexer,
	statsSvc *service.StatsService,
	clientRepo catHandler.OAuthClientLookup,
	tokenVerifier *oidctoken.Verifier,
	store devapi.Store,
	devCache *cache.RedisCache,
	newsSvc *newsService.PublicService,
	newsWriteSvc *newsService.SubmissionService,
	editRegistry *editing.Registry,
	playtimeSvc *service.UserPlaytimeService,
	workStateSvc *service.UserWorkStateService,
	folderSvc *service.UserFolderService,
	coverVoteSvc *service.CoverVoteService,
	claimSvc *service.ClaimLifecycleService,
	editEngine *editing.Engine,
) {
	oauthDB := application.DB.DB()

	repo := devapi.NewRepository(oauthDB)
	mw := devapi.NewMiddleware(repo, store)
	usageRec := devapi.NewUsageRecorder(repo, store)

	publicSvc := service.NewPublicService(catalogDB.DB(), readSvc, resolveSvc, cfg.ImageService.CDNBase)
	var imgCli *imageclient.Client
	if cfg.ImageClient.ClientID != "" && cfg.ImageClient.ClientSecret != "" {
		imgCli = imageclient.New(imageclient.Config{
			BaseURL:      cfg.ImageClient.BaseURL,
			CDNBase:      cfg.ImageService.CDNBase,
			ClientID:     cfg.ImageClient.ClientID,
			ClientSecret: cfg.ImageClient.ClientSecret,
		})
		publicSvc.WithImageMeta(func(ctx context.Context, hashes []string) (map[string]service.ImageMeta, error) {
			raw, err := imgCli.MetaBatch(ctx, hashes)
			if err != nil {
				return nil, err
			}
			out := make(map[string]service.ImageMeta, len(raw))
			for h, m := range raw {
				out[h] = service.ImageMeta{
					Width: m.Width, Height: m.Height, Thumbhash: m.Thumbhash, Sexual: m.Sexual,
				}
			}
			return out, nil
		})
	} else {
		slog.Warn("catalog public face: image client not configured — covers will carry no dimensions/thumbhash and the banner slot stays null")
	}
	// Edit-face byte upload (wave 169): the multipart leg the row-set editors
	// (covers/screenshots) obtain their hashes from. Deliberately NOT imgCli:
	// that identity is the wiki-era client the meta reads still ride, while
	// new bytes must land under the catalog SITE's own client — the scope the
	// daily catalog refping keeps out of the image GC. Unset credentials
	// disable the leg (503) rather than silently falling back to the wrong
	// site, which would strand every editor upload outside the refping sweep.
	var editUpload v2handler.EditImageUpload
	if cfg.CatalogImageClient.ClientID != "" && cfg.CatalogImageClient.ClientSecret != "" {
		editUpload = imageclient.New(imageclient.Config{
			BaseURL:      cfg.CatalogImageClient.BaseURL,
			CDNBase:      cfg.ImageService.CDNBase,
			ClientID:     cfg.CatalogImageClient.ClientID,
			ClientSecret: cfg.CatalogImageClient.ClientSecret,
		}).UploadWithSub
	} else {
		slog.Warn("catalog edit face: catalog image client not configured — editor image upload disabled (503)")
	}
	publicSvc.WithWorksSearch(searcher)

	var storeMinter storeService.Minter
	if cfg.Store.ShortlinkBaseURL != "" && cfg.Store.ShortlinkAPIKey != "" {
		storeMinter = shortener.New(cfg.Store.ShortlinkBaseURL, cfg.Store.ShortlinkAPIKey)
	} else {
		slog.Warn("store face: link shortener not configured — /v2/store answers 503 (set KUN_STORE_SHORTLINK_BASE_URL and KUN_STORE_SHORTLINK_API_KEY)")
	}
	if storeMinter != nil && !(storeService.ValidAffTemplate(cfg.Store.AffTemplateManiax) && storeService.ValidAffTemplate(cfg.Store.AffTemplatePro)) {
		slog.Warn("store face: affiliate template missing or without the {product_id} slot — purchase-link minting answers 503 (set KUN_STORE_DLSITE_AFF_URL_TMPL_MANIAX and _PRO)",
			"maniax_ready", storeService.ValidAffTemplate(cfg.Store.AffTemplateManiax),
			"pro_ready", storeService.ValidAffTemplate(cfg.Store.AffTemplatePro))
	}
	storeSvc := storeService.New(oauthDB, storeMinter, storeService.Options{
		AffTemplateManiax: cfg.Store.AffTemplateManiax,
		AffTemplatePro:    cfg.Store.AffTemplatePro,
	})
	var dlsiteProxy *url.URL
	if raw := cfg.Store.PriceDLsiteProxy; raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			slog.Error("store price: KUN_STORE_PRICE_DLSITE_PROXY is not a proxy URL (scheme://host:port)", "error", err)
			os.Exit(1)
		}
		dlsiteProxy = u
		slog.Info("store price: dlsite lane egresses via proxy", "scheme", u.Scheme, "host", u.Host)
	}
	priceSvc := price.New(oauthDB, []price.Fetcher{
		price.NewDLsite(keys.StorePriceDLsiteCurrencies.Get(), cfg.Store.PriceDLsiteBase, dlsiteProxy),
		price.NewSteam(keys.StorePriceSteamRegions.Get(), ""),
		price.NewGetchu(""),
	}, price.Options{})
	priceSvc.Start()

	// Wave R3 moved the metering here from the v1 groups. Deleting the v1 faces
	// removed every writer of developer_api_usage and every TouchLastUsed call,
	// which would have left the portal's usage panel and each key's "last used"
	// permanently empty with nothing failing.
	application.Fiber.Use("/v2", func(c fiber.Ctx) error {
		err := c.Next()
		if cred := devapi.CredentialFrom(c); cred != nil {
			usageRec.Record(cred, "v2", c.Route().Path, c.Response().StatusCode())
			go usageRec.TouchLastUsed(context.Background(), cred)
		}
		return err
	})

	bindingOfClient := func(ctx context.Context, clientID string) (v2handler.SiteBinding, error) {
		cl, err := clientRepo.FindByClientID(ctx, clientID)
		if err != nil || cl == nil {
			return v2handler.SiteBinding{}, err
		}
		return v2handler.SiteBinding{Site: cl.CatalogSite, ThirdParty: isThirdPartyEditClient(cl)}, nil
	}
	siteOfClient := func(ctx context.Context, clientID string) (string, error) {
		bind, err := bindingOfClient(ctx, clientID)
		return bind.Site, err
	}
	// Every service in the platform shares one HS256 secret and one JWK Set, so
	// a token this OP signed for anything else parsed here as a v2 user token.
	// KUN_SITE_URL sits in the shared compose env and is the OP's own issuer in
	// every process, which is what makes this checkable here at all.
	v2TokenVerifier := tokenVerifier.RequiringIssuer(cfg.OIDC.Issuer)

	v2API := v2handler.SetupWith(application.Fiber, v2handler.Options{
		Store:            protocol.NewRedisStore(devCache),
		LookupCredential: mw.Lookup,
		LookupUser: func(ctx context.Context, raw string) (v2handler.UserIdentity, error) {
			claims, err := v2TokenVerifier.Parse(ctx, raw)
			if err != nil {
				return v2handler.UserIdentity{}, err
			}
			return v2handler.UserIdentity{
				UID: int64(claims.ID), ClientID: claims.ClientID,
				Roles:  middleware.UnionRoles(claims.Roles, claims.SiteRoles),
				Scopes: strings.Fields(claims.Scope),
			}, nil
		},
		LookupSite: bindingOfClient,
		Catalog: &v2handler.Catalog{
			Public:      publicSvc,
			Resolve:     resolveSvc,
			StatsSvc:    statsSvc,
			News:        newsSvc,
			NewsWrite:   newsWriteSvc,
			Searcher:    searcher,
			EditTypes:   editRegistry,
			Playtime:    playtimeSvc,
			WorkStates:  workStateSvc,
			Folders:     folderSvc,
			CoverVotes:  coverVoteSvc,
			Claims:      claimSvc,
			Engine:      editEngine,
			EditHistory: service.NewEditHistoryService(catalogDB.DB()),
			Uploads:     v2handler.EditImageUpload(editUpload),
			Store:       storeSvc,
			Prices:      priceSvc,

			SiteOfAppClient: siteOfClient,
		},
	})
	v2spec, err := json.Marshal(v2API.OpenAPI())
	if err != nil {
		slog.Error("marshal catalog v2 spec", "error", err)
		os.Exit(1)
	}
	application.Fiber.Get("/v2/catalog/openapi.json", func(c fiber.Ctx) error {
		c.Set("Content-Type", "application/json")
		c.Set("Cache-Control", "public, max-age=3600")
		return c.Send(v2spec)
	})

	flushDone := make(chan struct{})
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-flushDone:
				return
			case <-t.C:
				if err := usageRec.Flush(context.Background()); err != nil {
					slog.Warn("devapi usage flush failed", "err", err)
				}
			}
		}
	}()
	application.Fiber.Hooks().OnPreShutdown(func() error {
		priceSvc.Stop()
		close(flushDone)
		if err := usageRec.Flush(context.Background()); err != nil {
			slog.Warn("devapi final usage flush failed", "err", err)
		}
		if devCache != nil {
			_ = devCache.Close()
		}
		return nil
	})
}
