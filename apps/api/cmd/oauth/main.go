package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"

	"api/internal/app"
	"api/internal/infrastructure/database"
	"api/internal/infrastructure/mail"
	"api/internal/jobs"
	"api/internal/middleware"
	"api/pkg/config"
	"api/pkg/health"
	"api/pkg/logger"
	"api/pkg/oidckeys"
	"api/pkg/oidctoken"
	"api/pkg/response"

	"api/internal/platform/auth/federation"
	authHandler "api/internal/platform/auth/handler"
	authRepo "api/internal/platform/auth/repository"
	authService "api/internal/platform/auth/service"
	"api/pkg/imageclient"

	artifactHandler "api/internal/platform/artifact/handler"
	artifactRepo "api/internal/platform/artifact/repository"
	artifactStorage "api/internal/platform/artifact/storage"

	"api/internal/platform/devapi"
	devapiPerm "api/internal/platform/devapi/perm"
	"api/internal/platform/permissions"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	settingsPerm "api/internal/platform/settings/perm"

	imgHandler "api/internal/platform/image/handler"
	imgRepoPkg "api/internal/platform/image/repository"
	imgService "api/internal/platform/image/service"
	imgStorage "api/internal/platform/image/storage"

	siteHandler "api/internal/platform/site/handler"
	sitePerm "api/internal/platform/site/perm"
	siteRepo "api/internal/platform/site/repository"
	siteService "api/internal/platform/site/service"
	storeHandler "api/internal/platform/store/handler"
	storeService "api/internal/platform/store/service"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	health.MaybeProbe(cfg.Server.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{
		Name:      "kun-oauth",
		NeedCache: true,
	})
	if err != nil {
		slog.Error("failed to create application", "error", err)
		os.Exit(1)
	}

	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	defer cancelCleanup()

	setupRoutes(application, cfg, cleanupCtx)

	if err := application.Run(cfg.Server.Host, cfg.Server.Port); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}
}

func setupRoutes(a *app.App, cfg *config.Config, cleanupCtx context.Context) {
	db := a.DB.DB()

	userRepo := authRepo.NewUserRepository(db)
	sessionRepo := authRepo.NewSessionRepository(db)
	passwordResetRepo := authRepo.NewPasswordResetRepository(db)
	authCodeRepo := authRepo.NewAuthorizationCodeRepository(db)
	siteRoleRepo := authRepo.NewUserSiteRoleRepository(db)
	oauthClientRepo := siteRepo.NewOAuthClientRepository(db)
	siteRepository := siteRepo.NewSiteRepository(db)
	signingKeyRepo := authRepo.NewSigningKeyRepository(db)

	app.StartCleanup(cleanupCtx, sessionRepo, authCodeRepo)

	mailer := mail.NewMailer(cfg.Mail)

	var imgCli *imageclient.Client
	if cfg.ImageClient.ClientID != "" && cfg.ImageClient.ClientSecret != "" {
		imgCli = imageclient.New(imageclient.Config{
			BaseURL:      cfg.ImageClient.BaseURL,
			CDNBase:      cfg.ImageService.CDNBase,
			ClientID:     cfg.ImageClient.ClientID,
			ClientSecret: cfg.ImageClient.ClientSecret,
		})
		slog.Info("image client configured for avatar uploads + GC")
	} else {
		slog.Warn("image client not configured; avatar upload + avatar GC disabled")
	}

	authSvc := authService.NewAuthServiceFull(userRepo, sessionRepo, passwordResetRepo, mailer, a.Cache, cfg)
	oauthSvc := authService.NewOAuthService(userRepo, authCodeRepo, sessionRepo, oauthClientRepo, siteRoleRepo, cfg)
	adminSvc := authService.NewAdminService(userRepo, sessionRepo, siteRoleRepo, siteRepository, imgCli)
	userBatchSvc := authService.NewUserBatchService(userRepo, siteRoleRepo)
	creatorAppSvc := authService.NewCreatorApplicationService(authRepo.NewCreatorApplicationRepository(db), userRepo, userBatchSvc)
	moemoepointSvc := authService.NewMoemoepointService(a.DB.DB(), userRepo)
	authSvc.WithMoemoepoint(moemoepointSvc)

	fedReg := federation.NewRegistry(cfg)
	oauthAccountRepo := authRepo.NewOAuthAccountRepository(db)
	fedSvc := authService.NewFederationService(authSvc, userRepo, oauthAccountRepo, sessionRepo, a.Cache, cfg, fedReg)
	slog.Info("federation providers configured", "count", fedReg.ConfiguredCount())

	devRepo := devapi.NewRepository(db)
	siteSvc := siteService.NewSiteService(siteRepository, oauthClientRepo, devRepo)

	authH := authHandler.NewAuthHandler(authSvc, cfg)
	fedH := authHandler.NewFederationHandler(fedSvc, cfg)
	oauthH := authHandler.NewOAuthHandler(oauthSvc, cfg)
	adminH := authHandler.NewAdminHandler(adminSvc)
	moemoepointH := authHandler.NewMoemoepointHandler(moemoepointSvc)
	userBatchH := authHandler.NewUserBatchHandler(userBatchSvc)
	creatorAppH := authHandler.NewCreatorApplicationHandler(creatorAppSvc)

	var avatarUploadH *authHandler.AvatarUploadHandler
	if imgCli != nil {
		avatarUploadH = authHandler.NewAvatarUploadHandler(a.DB.DB(), imgCli)
	}
	siteH := siteHandler.NewSiteHandler(siteSvc)

	var oidcH *authHandler.OIDCHandler
	if cfg.OIDC.KeyEncKey != "" {
		signingKeySvc := authService.NewSigningKeyService(signingKeyRepo, cfg.OIDC.KeyEncKey)
		if err := signingKeySvc.EnsureBootstrapped(cleanupCtx); err != nil {
			slog.Error("oidc signing key bootstrap failed", "err", err)
		}
		oidcH = authHandler.NewOIDCHandler(signingKeySvc, cfg)

		verifier := oidctoken.NewVerifier(cfg.JWT.Secret, signingKeySvc)
		var signer oidctoken.Signer = oidctoken.NewHS256Signer(cfg.JWT.Secret, cfg.OIDC.Issuer)
		if kid, key, err := signingKeySvc.ActiveSigner(cleanupCtx, oidckeys.AlgRS256); err != nil {
			slog.Error("no active RS256 signing key; id_token issuance disabled", "err", err)
		} else {
			oauthSvc.WithIDSigner(oidctoken.NewIDSigner(kid, oidckeys.AlgRS256, key, cfg.OIDC.Issuer))
		}
		if kid, key, err := signingKeySvc.ActiveSigner(cleanupCtx, oidckeys.AlgES256); err != nil {
			slog.Error("no active ES256 signing key; asymmetric access-token signing unavailable", "err", err)
		} else if cfg.OIDC.SignAsymmetric {
			signer = oidctoken.NewES256Signer(kid, key, cfg.OIDC.Issuer)
			slog.Info("oauth signing access tokens with ES256", "kid", kid)
		}
		authSvc.WithTokenSigner(signer).WithTokenVerifier(verifier)
		oauthSvc.WithTokenSigner(signer)
	} else {
		slog.Warn("KUN_OIDC_KEY_ENC_KEY unset; OIDC signing keys + jwks/discovery disabled")
	}

	a.Fiber.Use(middleware.RequestID())
	a.Fiber.Use(middleware.Logger())

	a.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	if oidcH != nil {
		a.Fiber.Get("/oauth/jwks", oidcH.JWKS)
		a.Fiber.Get("/.well-known/openid-configuration", oidcH.Discovery)
		a.Fiber.Get("/.well-known/oauth-authorization-server", oidcH.Discovery)
	}

	devStore := devapi.NewRedisStore(a.Cache)
	devMW := devapi.NewMiddleware(devRepo, devStore)
	usageRec := devapi.NewUsageRecorder(devRepo, devStore)
	fwdAuth := devapi.NewForwardAuth(devMW, usageRec)
	// Before CORS and the house IP limiter: this handler never calls Next, so
	// Traefik's single egress identity would otherwise share one rate bucket
	// across every downstream face. Same placement as /healthz and /oauth/jwks.
	a.Fiber.Get("/internal/devapi/forward-auth", fwdAuth.Handle)

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
	a.Fiber.Hooks().OnPreShutdown(func() error {
		close(flushDone)
		if err := usageRec.Flush(context.Background()); err != nil {
			slog.Warn("devapi final usage flush failed", "err", err)
		}
		return nil
	})

	a.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))
	a.Fiber.Use(middleware.RateLimit(a.Cache))

	api := a.Fiber.Group("/api")
	v1 := api.Group("/v1")

	strict := middleware.StrictRateLimit(a.Cache)

	oauthTokenLimiter := middleware.OAuthTokenRateLimit(a.Cache)

	auth := v1.Group("/auth")
	auth.Post("/register/send-code", strict, authH.SendRegisterCode)
	auth.Post("/register", strict, authH.Register)
	auth.Post("/login", strict, authH.Login)
	auth.Post("/refresh", authH.Refresh)
	auth.Post("/password/forgot", strict, authH.ForgotPassword)
	auth.Post("/password/reset", strict, authH.ResetPassword)

	fed := auth.Group("/federation")
	fed.Get("/providers", fedH.Providers)
	fed.Get("/:provider/start", strict, fedH.Start)
	fed.Get("/:provider/callback", strict, fedH.Callback)
	fed.Get("/pending", strict, fedH.Pending)
	fed.Post("/complete", strict, fedH.Complete)

	authProtected := auth.Group("", middleware.Auth(authSvc))
	authProtected.Post("/logout", authH.Logout)
	authProtected.Get("/me", authH.Me)
	authProtected.Get("/sessions", authH.ListSessions)
	authProtected.Post("/sessions/switch", authH.SwitchSession)
	authProtected.Post("/sessions/logout", authH.LogoutAccount)
	authProtected.Post("/sessions/logout-all", authH.LogoutAll)
	authProtected.Patch("/me", authH.UpdateProfile)
	authProtected.Get("/me/moemoepoint/log", moemoepointH.MyLog)
	authProtected.Put("/password", authH.ChangePassword)
	authProtected.Post("/email/send-code", authH.SendEmailChangeCode)
	authProtected.Put("/email", authH.ChangeEmail)
	if avatarUploadH != nil {
		authProtected.Post("/me/avatar", avatarUploadH.UploadMine)
	}

	oauth := v1.Group("/oauth")
	oauth.Post("/token", oauthTokenLimiter, oauthH.Token)
	oauth.Post("/revoke", oauthH.Revoke)
	oauth.Get("/authorize", oauthH.Authorize)
	oauth.Get("/client-info", oauthH.GetClientPublic)
	oauth.Post("/authorize/error", oauthH.AuthorizeError)
	oauth.Get("/ecosystem", oauthH.GetEcosystem)
	oauth.Get("/post-logout-redirect", oauthH.PostLogoutRedirect)
	oauth.Get("/logout", oauthH.LogoutRedirect)
	oauth.Post("/logout", oauthH.LogoutRedirect)
	// Per-route guards, deliberately NOT a group. `oauth.Group("", mw)` attaches
	// mw to the /oauth prefix itself, so it silently applies to every route
	// registered on `oauth` AFTERWARDS too — which is how /userinfo ended up
	// behind the house guard and answering the {code,message} envelope even
	// though it names BearerAuth. Naming the middleware on each route removes
	// the ordering hazard entirely.
	//
	// /oauth/authorize/consent is our own browser flow → house guard, house body.
	oauth.Post("/authorize/consent", middleware.Auth(authSvc), oauthH.Consent)
	oauth.Get("/userinfo", middleware.BearerAuth(authSvc), oauthH.UserInfo)

	v1.Get("/users/batch",
		middleware.OAuthClientBasicAuth(oauthClientRepo),
		userBatchH.Get,
	)
	v1.Get("/users/search",
		middleware.OAuthClientBasicAuth(oauthClientRepo),
		userBatchH.Search,
	)
	v1.Post("/users/:id/moemoepoint",
		middleware.OAuthClientBasicAuth(oauthClientRepo), moemoepointH.Adjust)
	v1.Get("/users/:id/moemoepoint",
		middleware.OAuthClientBasicAuth(oauthClientRepo), moemoepointH.GetBalance)
	v1.Get("/users/:id/moemoepoint/log",
		middleware.OAuthClientBasicAuth(oauthClientRepo), moemoepointH.GetLog)

	users := v1.Group("/users", middleware.Auth(authSvc))
	users.Get("/:uuid", authH.GetProfile)

	creator := v1.Group("/creator", middleware.Auth(authSvc))
	creator.Post("/applications", creatorAppH.Apply)
	creator.Get("/applications/me", creatorAppH.MyApplication)

	admin := v1.Group("/admin", middleware.Auth(authSvc), middleware.RequirePermission(sitePerm.Resolver, sitePerm.AdminAccess))
	admin.Get("/users", adminH.ListUsers)
	admin.Get("/users/:uuid", adminH.GetUser)
	admin.Patch("/users/:uuid", adminH.UpdateUser)
	admin.Post("/users/:uuid/ban", adminH.BanUser)
	admin.Post("/users/:uuid/unban", adminH.UnbanUser)
	admin.Post("/users/:uuid/anonymize", adminH.AnonymizeUser)
	admin.Delete("/users/:uuid/sessions", adminH.DeleteUserSessions)
	admin.Post("/users/:uuid/roles", adminH.AssignRole)
	admin.Delete("/users/:uuid/roles/:role", adminH.RevokeRole)
	admin.Post("/users/:uuid/site-roles",
		middleware.RequirePermission(sitePerm.Resolver, sitePerm.RolesGrantSite), adminH.AssignSiteRole)
	admin.Delete("/users/:uuid/site-roles",
		middleware.RequirePermission(sitePerm.Resolver, sitePerm.RolesGrantSite), adminH.RevokeSiteRole)
	admin.Get("/creator/applications", creatorAppH.AdminList)
	admin.Post("/creator/applications/:id/approve", creatorAppH.AdminApprove)
	admin.Post("/creator/applications/:id/decline", creatorAppH.AdminDecline)
	admin.Post("/users/:uuid/moemoepoint", moemoepointH.AdminAdjust)
	admin.Get("/users/:uuid/moemoepoint/log", moemoepointH.AdminGetLog)
	admin.Get("/stats/registrations", adminH.RegistrationStats)
	admin.Get("/stats/registrations/hourly", adminH.HourlyRegistrationStats)
	if avatarUploadH != nil {
		admin.Post("/users/:uuid/avatar", avatarUploadH.Upload)
	}

	sites := v1.Group("/sites", middleware.Auth(authSvc), middleware.RequirePermission(sitePerm.Resolver, sitePerm.AdminAccess))
	sites.Get("/", siteH.List)
	sites.Post("/", middleware.RequirePermission(sitePerm.Resolver, sitePerm.SitesCreate), siteH.Create)
	sites.Get("/:id", siteH.Get)
	sites.Put("/:id", middleware.RequirePermission(sitePerm.Resolver, sitePerm.SitesUpdate), siteH.Update)
	sites.Delete("/:id", middleware.RequirePermission(sitePerm.Resolver, sitePerm.SitesDelete), siteH.Delete)
	sites.Get("/:id/clients", siteH.GetSiteClients)

	oauthClients := v1.Group("/oauth/clients", middleware.Auth(authSvc), middleware.RequirePermission(sitePerm.Resolver, sitePerm.AdminAccess))
	oauthClients.Get("/", siteH.ListClients)
	oauthClients.Post("/", middleware.RequirePermission(sitePerm.Resolver, sitePerm.ClientsCreate), siteH.CreateClient)
	if avatarUploadH != nil {
		oauthClients.Post("/logo", avatarUploadH.UploadClientLogo)
	}
	oauthClients.Put("/:id", middleware.RequirePermission(sitePerm.Resolver, sitePerm.ClientsUpdate), siteH.UpdateClient)
	oauthClients.Put("/:id/storage",
		middleware.RequirePermission(sitePerm.Resolver, sitePerm.ClientsUpdate),
		middleware.RequirePermission(sitePerm.Resolver, sitePerm.ClientsStorageConfig),
		siteH.UpdateClientStorage)
	oauthClients.Delete("/:id", middleware.RequirePermission(sitePerm.Resolver, sitePerm.ClientsDelete), siteH.DeleteClient)

	devAdminSvc := devapi.NewAdminService(devRepo, devStore)
	devAdminH := devapi.NewAdminHandler(devAdminSvc)
	devGroup := admin.Group("/devapi", middleware.RequirePermission(devapiPerm.Resolver, devapiPerm.Manage))
	devAdminH.Register(devGroup, middleware.RequirePermission(devapiPerm.Resolver, devapiPerm.PolicyManage))

	devSelfH := devapi.NewSelfServiceHandler(devapi.NewSelfServiceService(devRepo, devAdminSvc, devStore))
	devPortalClients := make(map[string]bool, len(cfg.DevPortalClientIDs))
	for _, id := range cfg.DevPortalClientIDs {
		devPortalClients[id] = true
	}
	devSelfGroup := v1.Group("/dev", middleware.Auth(authSvc), middleware.DevPortalFence(devPortalClients))
	devSelfH.Register(devSelfGroup)

	storeHandler.NewDevHandler(
		storeService.New(db, nil, storeService.Options{}),
		func(ctx context.Context, ownerUserID uint) ([]storeService.OwnerApp, error) {
			apps, err := devRepo.ListAppsByOwner(ctx, ownerUserID)
			if err != nil {
				return nil, err
			}
			out := make([]storeService.OwnerApp, len(apps))
			for i := range apps {
				out[i] = storeService.OwnerApp{ClientID: apps[i].ID, Name: apps[i].Name}
			}
			return out, nil
		},
	).Register(devSelfGroup)

	permReg := permissions.Live()
	permDist := permissions.NewDistributor(db, permReg, a.Cache)
	permDist.Start(cleanupCtx)
	permH := permissions.NewHandler(permissions.NewService(permReg, permissions.NewStore(db), permDist))
	permH.Register(admin)

	settingsReg := keys.Live()
	settingsDist := settings.NewDistributor(db, settingsReg, a.Cache)
	settingsDist.Start(cleanupCtx)
	settingsH := settings.NewHandler(
		settings.NewService(settingsReg, settings.NewStore(db), settingsDist),
		func(roles []string) bool { return settingsPerm.Resolver.Can(roles, settingsPerm.Write) },
		func(ctx context.Context, id uint) (bool, error) {
			_, err := siteRepository.FindByID(ctx, id)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return true, nil
		},
	)
	settingsH.Register(admin,
		middleware.RequirePermission(settingsPerm.Resolver, settingsPerm.View),
		middleware.RequirePermission(settingsPerm.Resolver, settingsPerm.Write))
	settingsH.RegisterS2S(v1, middleware.OAuthClientBasicAuth(oauthClientRepo), func(c fiber.Ctx) *uint {
		if client := middleware.OAuthClientFromCtx(c); client != nil {
			return client.SiteID
		}
		return nil
	})

	registerImageAdmin(a, cfg, admin)
	registerArtifactAdmin(a, cfg, authSvc)

	jobReg := jobs.NewRegistry()
	jobs.RegisterAll(jobReg)
	jobRunner := jobs.NewRunner(cfg, db)
	jobs.StartScheduler(cleanupCtx, jobReg, jobRunner)
	registerJobsAdmin(admin, jobReg, jobRunner)
}

func registerJobsAdmin(admin fiber.Router, reg *jobs.Registry, runner *jobs.Runner) {
	g := admin.Group("/jobs")

	g.Get("", func(c fiber.Ctx) error {
		type jobView struct {
			Name      string `json:"name"`
			Desc      string `json:"desc"`
			Schedule  string `json:"schedule"`
			Enabled   bool   `json:"enabled"`
			LatestRun any    `json:"latest_run"`
		}
		out := make([]jobView, 0)
		for _, j := range reg.List() {
			latest, err := runner.LatestRun(c.Context(), j.Name)
			if err != nil {
				slog.Error("jobs admin: latest run", "job", j.Name, "err", err)
			}
			jk, _ := keys.Job(j.Name)
			out = append(out, jobView{
				Name:      j.Name,
				Desc:      j.Desc,
				Schedule:  jk.Schedule.Get(),
				Enabled:   jk.Enabled.Get(),
				LatestRun: latest,
			})
		}
		return response.Success(c, out)
	})

	g.Post("/:name/run", func(c fiber.Ctx) error {
		name := c.Params("name")
		job, ok := reg.Get(name)
		if !ok {
			return response.Error(c, fiber.StatusNotFound, fiber.StatusNotFound, "unknown job: "+name)
		}
		runner.RunAsync(job, jobs.TriggerAdmin)
		return response.SuccessWithMessage(c, "job triggered (running in background)", fiber.Map{"job": name})
	})

	g.Get("/:name/runs", func(c fiber.Ctx) error {
		name := c.Params("name")
		if _, ok := reg.Get(name); !ok {
			return response.Error(c, fiber.StatusNotFound, fiber.StatusNotFound, "unknown job: "+name)
		}
		limit, _ := strconv.Atoi(c.Query("limit"))
		runs, err := runner.ListRuns(c.Context(), name, limit)
		if err != nil {
			slog.Error("jobs admin: list runs", "job", name, "err", err)
			return response.Error(c, fiber.StatusInternalServerError, fiber.StatusInternalServerError, "failed to list runs")
		}
		return response.Success(c, runs)
	})

	slog.Info("jobs admin endpoints registered under /api/v1/admin/jobs/*")
}

func registerImageAdmin(_ *app.App, cfg *config.Config, admin fiber.Router) {
	imagesDB, err := database.NewPostgresDB(cfg.ImagesDatabase)
	if err != nil {
		slog.Warn("image admin: images db unreachable; admin endpoints disabled", "err", err)
		return
	}
	s3, err := imgStorage.NewClient(cfg.ImageS3)
	if err != nil {
		slog.Warn("image admin: s3 unreachable; admin endpoints disabled", "err", err)
		return
	}

	imgRepo := imgRepoPkg.NewImageRepository(imagesDB.DB())
	usageRepo := imgRepoPkg.NewSiteUsageRepository(imagesDB.DB())
	statsRepo := imgRepoPkg.NewStatsRepository(imagesDB.DB())
	svc := imgService.New(nil, s3, imgRepo, usageRepo, cfg.ImageService.CDNBase)
	adminH := imgHandler.NewAdmin(imagesDB.DB(), svc, statsRepo, s3)

	g := admin.Group("/image")
	g.Get("/list", adminH.List)
	g.Get("/stats", adminH.Stats)
	g.Patch("/:hash/review", adminH.Review)
	g.Delete("/:hash", adminH.Delete)

	slog.Info("image admin endpoints registered under /api/v1/admin/image/*")
}

func registerArtifactAdmin(a *app.App, cfg *config.Config, authSvc *authService.AuthService) {
	artifactsDB, err := database.NewPostgresDB(cfg.ArtifactsDatabase)
	if err != nil {
		slog.Warn("artifact admin: artifacts db unreachable; admin endpoints disabled", "err", err)
		return
	}
	statsRepo := artifactRepo.NewStatsRepository(artifactsDB.DB())

	var store *artifactStorage.Client
	if cfg.ArtifactS3.AccessKeyID != "" {
		if c, serr := artifactStorage.NewClient(cfg.ArtifactCleanupS3()); serr != nil {
			slog.Warn("artifact admin: cleanup storage init failed; reclaim disabled", "err", serr)
		} else {
			store = c
		}
	}
	adminH := artifactHandler.NewAdmin(artifactsDB.DB(), statsRepo, store)

	a.Fiber.Use("/api/v1/admin/artifact", middleware.Auth(authSvc), middleware.RequirePermission(sitePerm.Resolver, sitePerm.AdminAccess))
	artifactHandler.SetupAdmin(a.Fiber, adminH)

	slog.Info("artifact admin endpoints (Huma) registered under /api/v1/admin/artifact/*")
}
