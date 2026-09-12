package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Server                  ServerConfig
	Database                DatabaseConfig
	GalgameDatabase         DatabaseConfig
	CatalogDatabase         DatabaseConfig
	CommunityDatabase       DatabaseConfig
	TrustDatabase           DatabaseConfig
	AIDatabase              DatabaseConfig
	NewsDatabase            DatabaseConfig
	ImagesDatabase          DatabaseConfig
	Redis                   RedisConfig
	JWT                     JWTConfig
	OIDC                    OIDCConfig
	Federation              FederationConfig
	Mail                    MailConfig
	OpenSearch              OpenSearchConfig
	ImageService            ImageServiceConfig
	ImageS3                 S3Config
	ImageClient             ImageClientConfig
	CatalogClient           CatalogClientConfig
	TrustClient             TrustClientConfig
	TrustCallbackSecret     string
	TrustForwarderClientIDs []string
	DevPortalClientIDs      []string
	// GalgameImageClient is a SECOND image client identity, used only by the
	// galgame-image-refping job. The job runs in the oauth container (central
	// scheduler), where ImageClient is the *account* client — but galgame
	// banners/covers were uploaded under the *galgame_wiki* site, and
	// reference-ping is site-scoped, so pinging as account silently 404s every
	// hash. This lets the oauth container ping as galgame_wiki. Falls back to
	// ImageClient when unset (the standalone cmd in the galgame container,
	// where ImageClient already == galgame_wiki, needs no extra env).
	GalgameImageClient ImageClientConfig

	CatalogImageClient ImageClientConfig

	NewsImageClient ImageClientConfig

	NewsModeration NewsModerationConfig

	Ymgal YmgalConfig

	ArtifactsDatabase DatabaseConfig
	ArtifactS3        S3Config
	ArtifactService   ArtifactServiceConfig

	CatalogService   CatalogServiceConfig
	CommunityService CommunityServiceConfig
	TrustService     TrustServiceConfig

	AIService  AIServiceConfig
	AIUpstream AIUpstreamConfig
	AIOmni     AIOmniConfig

	AIClient AIClientConfig

	Store StoreConfig
}

// StoreConfig is the DLsite distribution face: how the platform reaches the
// link shortener, and the affiliate URL templates the short links point at.
// The aff id lives inside the templates because it is a commercial value the
// deployment supplies, never a constant in the source.
type StoreConfig struct {
	ShortlinkBaseURL  string
	ShortlinkAPIKey   string
	AffTemplateManiax string
	AffTemplatePro    string
	PriceDLsiteBase   string
	PriceDLsiteProxy  string
}

type AIClientConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

// NewsModerationConfig is how the news moderation job reaches trust (Tier0) and
// the AI gateway. Both services derive the caller's site from the OAuth client's
// catalog_site, which is what scopes the Tier0 word list and the AI spend, so
// these calls must be made as the news client and not as a generic first-party
// one. The credentials default to the news image client because it is the same
// oauth_clients row — one machine identity for the whole track — with the env
// pair kept as an escape hatch if the two ever need splitting.
type NewsModerationConfig struct {
	TrustBaseURL string
	AIBaseURL    string
	ClientID     string
	ClientSecret string
}

type AIServiceConfig struct {
	Host string
	Port int
}

type AIUpstreamConfig struct {
	BaseURL string
	Token   string
}

type AIOmniConfig struct {
	BaseURL string
	Token   string
}

type TrustServiceConfig struct {
	Host string
	Port int
}

type CommunityServiceConfig struct {
	Host string
	Port int
}

type CatalogServiceConfig struct {
	Host string
	Port int
}

type ArtifactServiceConfig struct {
	Host string
	Port int

	CleanupAccessKey string
	CleanupSecretKey string
}

type ImageClientConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

type FederationProviderConfig struct {
	ClientID     string
	ClientSecret string
}

type FederationConfig struct {
	Google FederationProviderConfig
	GitHub FederationProviderConfig
}

// YmgalConfig is 月幕 Galgame's OpenAPI client. Use the dedicated client 苍麟
// issued us, never the credentials published on their developer page: those are
// shared by every developer and therefore share one rate-limit bucket, which is
// someone else's production dependency.
type YmgalConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

type CatalogClientConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

type TrustClientConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

type ImageServiceConfig struct {
	Host        string
	Port        int
	CDNBase     string
	PresetsPath string
}

type S3Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	UsePathStyle    bool
}

type OpenSearchConfig struct {
	Host        string
	IndexPrefix string
}

type MailConfig struct {
	From     string
	Host     string
	Port     int
	Account  string
	Password string
}

type ServerConfig struct {
	Host        string
	Port        int
	Env         string
	SiteURL     string
	FrontendURL string
	CORSOrigin  string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
	Timezone string

	Pool PoolConfig
}

type PoolConfig struct {
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

type RedisConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	Password string
	DB       int
}

type JWTConfig struct {
	Secret     string
	CookieName string
	Expires    string
}

// OIDCConfig holds the OIDC OpenID Provider settings.
//
// Issuer is the canonical identifier (== Server.SiteURL) used as the token
// `iss`, the discovery `issuer`, and the base the .well-known documents are
// served from — all three MUST match (a classic OIDC footgun).
//
// KeyEncKey (KUN_OIDC_KEY_ENC_KEY) is the KEK that encrypts signing private
// keys at rest. Only cmd/oauth needs it — it generates/decrypts keys; the
// separate resource-server binaries verify with public keys only. When empty,
// cmd/oauth skips signing-key bootstrap + the jwks/discovery endpoints.
//
// Design: docs/auth/03-oidc-standardization-design.md.
type OIDCConfig struct {
	Issuer         string
	KeyEncKey      string
	SignAsymmetric bool
	JWKSURL        string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}

	serverPort, _ := strconv.Atoi(getEnv("KUN_FIBER_SERVER_PORT", "9277"))
	cfg.Server = ServerConfig{
		Host:        getEnv("KUN_FIBER_SERVER_HOST", "127.0.0.1"),
		Port:        serverPort,
		Env:         getEnv("KUN_ENV", "development"),
		SiteURL:     getEnv("KUN_SITE_URL", "http://127.0.0.1:9277"),
		FrontendURL: getEnv("KUN_FRONTEND_URL", "http://127.0.0.1:9420"),
		CORSOrigin:  getEnv("KUN_FRONTEND_CORS_ORIGIN", "http://127.0.0.1:9420,http://127.0.0.1:9421"),
	}

	cfg.Database = DatabaseConfig{
		Host:     getEnv("KUN_PG_HOST", "localhost"),
		Port:     getEnv("KUN_PG_PORT", "5432"),
		User:     getEnv("KUN_PG_USER", "postgres"),
		Password: getEnv("KUN_PG_PASSWORD", ""),
		DBName:   getEnv("KUN_PG_DATABASE", "kun_galgame_infra"),
		SSLMode:  getEnv("KUN_PG_SSLMODE", "disable"),
		Timezone: getEnv("KUN_PG_TIMEZONE", "Asia/Shanghai"),
	}

	cfg.GalgameDatabase = DatabaseConfig{
		Host:     getEnv("KUN_GALGAME_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_GALGAME_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_GALGAME_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_GALGAME_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_GALGAME_PG_DATABASE", "kun_catalog"),
		SSLMode:  getEnv("KUN_GALGAME_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_GALGAME_PG_TIMEZONE", cfg.Database.Timezone),
	}

	cfg.CatalogDatabase = DatabaseConfig{
		Host:     getEnv("KUN_CATALOG_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_CATALOG_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_CATALOG_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_CATALOG_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_CATALOG_PG_DATABASE", "kun_catalog"),
		SSLMode:  getEnv("KUN_CATALOG_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_CATALOG_PG_TIMEZONE", cfg.Database.Timezone),
	}

	cfg.CommunityDatabase = DatabaseConfig{
		Host:     getEnv("KUN_COMMUNITY_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_COMMUNITY_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_COMMUNITY_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_COMMUNITY_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_COMMUNITY_PG_DATABASE", "kun_community"),
		SSLMode:  getEnv("KUN_COMMUNITY_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_COMMUNITY_PG_TIMEZONE", cfg.Database.Timezone),
	}

	cfg.TrustDatabase = DatabaseConfig{
		Host:     getEnv("KUN_TRUST_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_TRUST_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_TRUST_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_TRUST_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_TRUST_PG_DATABASE", "kun_trust"),
		SSLMode:  getEnv("KUN_TRUST_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_TRUST_PG_TIMEZONE", cfg.Database.Timezone),
	}

	cfg.AIDatabase = DatabaseConfig{
		Host:     getEnv("KUN_AI_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_AI_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_AI_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_AI_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_AI_PG_DATABASE", "kun_ai"),
		SSLMode:  getEnv("KUN_AI_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_AI_PG_TIMEZONE", cfg.Database.Timezone),
	}

	cfg.NewsDatabase = DatabaseConfig{
		Host:     getEnv("KUN_NEWS_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_NEWS_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_NEWS_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_NEWS_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_NEWS_PG_DATABASE", "kun_news"),
		SSLMode:  getEnv("KUN_NEWS_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_NEWS_PG_TIMEZONE", cfg.Database.Timezone),
	}

	redisEnabled, _ := strconv.ParseBool(getEnv("REDIS_ENABLED", "false"))
	redisPort, _ := strconv.Atoi(getEnv("REDIS_PORT", "6379"))
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	cfg.Redis = RedisConfig{
		Enabled:  redisEnabled,
		Host:     getEnv("REDIS_HOST", "localhost"),
		Port:     redisPort,
		Username: getEnv("REDIS_USERNAME", ""),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       redisDB,
	}

	cfg.JWT = JWTConfig{
		Secret:     getEnv("JWT_SECRET", ""),
		CookieName: getEnv("JWT_COOKIE_NAME", "kun_token"),
		Expires:    getEnv("JWT_EXPIRES", "90d"),
	}

	signAsym, _ := strconv.ParseBool(getEnv("KUN_OIDC_SIGN_ASYMMETRIC", "false"))
	cfg.OIDC = OIDCConfig{
		Issuer:         cfg.Server.SiteURL,
		KeyEncKey:      getEnv("KUN_OIDC_KEY_ENC_KEY", ""),
		SignAsymmetric: signAsym,
		JWKSURL:        getEnv("KUN_OIDC_JWKS_URL", ""),
	}

	mailPort, _ := strconv.Atoi(getEnv("KUN_VISUAL_NOVEL_EMAIL_PORT", "587"))
	cfg.Mail = MailConfig{
		From:     getEnv("KUN_VISUAL_NOVEL_EMAIL_FROM", "NextMoe·未萌"),
		Host:     getEnv("KUN_VISUAL_NOVEL_EMAIL_HOST", ""),
		Port:     mailPort,
		Account:  getEnv("KUN_VISUAL_NOVEL_EMAIL_ACCOUNT", ""),
		Password: getEnv("KUN_VISUAL_NOVEL_EMAIL_PASSWORD", ""),
	}

	cfg.OpenSearch = OpenSearchConfig{
		Host:        getEnv("KUN_OPENSEARCH_HOST", "http://127.0.0.1:9200"),
		IndexPrefix: getEnv("KUN_OPENSEARCH_INDEX_PREFIX", ""),
	}

	cfg.ImagesDatabase = DatabaseConfig{
		Host:     getEnv("KUN_IMAGES_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_IMAGES_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_IMAGES_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_IMAGES_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_IMAGES_PG_DATABASE", "kun_images"),
		SSLMode:  getEnv("KUN_IMAGES_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_IMAGES_PG_TIMEZONE", cfg.Database.Timezone),
	}

	imagePort, _ := strconv.Atoi(getEnv("KUN_IMAGE_SERVICE_PORT", "9278"))
	cfg.ImageService = ImageServiceConfig{
		Host:        getEnv("KUN_IMAGE_SERVICE_HOST", "127.0.0.1"),
		Port:        imagePort,
		CDNBase:     getEnv("KUN_IMAGE_PUBLIC_BASE_URL", "http://127.0.0.1:9000/kun-images-dev"),
		PresetsPath: getEnv("KUN_IMAGE_PRESETS_PATH", "apps/api/configs/image_presets.yaml"),
	}

	s3UsePathStyle, _ := strconv.ParseBool(getEnv("KUN_IMAGE_S3_FORCE_PATH_STYLE", "true"))
	cfg.ImageS3 = S3Config{
		Endpoint:        getEnv("KUN_IMAGE_S3_ENDPOINT", "http://127.0.0.1:9000"),
		Region:          getEnv("KUN_IMAGE_S3_REGION", "us-east-1"),
		AccessKeyID:     getEnv("KUN_IMAGE_S3_ACCESS_KEY", ""),
		SecretAccessKey: getEnv("KUN_IMAGE_S3_SECRET_KEY", ""),
		Bucket:          getEnv("KUN_IMAGE_S3_BUCKET", "kun-images-dev"),
		UsePathStyle:    s3UsePathStyle,
	}

	defaultBase := fmt.Sprintf("http://%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
	cfg.ImageClient = ImageClientConfig{
		BaseURL:      getEnv("KUN_IMAGE_CLIENT_BASE_URL", defaultBase),
		ClientID:     getEnv("KUN_IMAGE_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_IMAGE_CLIENT_SECRET", ""),
	}

	cfg.Federation = FederationConfig{
		Google: FederationProviderConfig{
			ClientID:     getEnv("KUN_FEDERATION_GOOGLE_CLIENT_ID", ""),
			ClientSecret: getEnv("KUN_FEDERATION_GOOGLE_CLIENT_SECRET", ""),
		},
		GitHub: FederationProviderConfig{
			ClientID:     getEnv("KUN_FEDERATION_GITHUB_CLIENT_ID", ""),
			ClientSecret: getEnv("KUN_FEDERATION_GITHUB_CLIENT_SECRET", ""),
		},
	}

	cfg.CatalogClient = CatalogClientConfig{
		BaseURL:      getEnv("KUN_CATALOG_CLIENT_BASE_URL", "http://catalog:9281"),
		ClientID:     getEnv("KUN_CATALOG_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_CATALOG_CLIENT_SECRET", ""),
	}

	cfg.TrustClient = TrustClientConfig{
		BaseURL:      getEnv("KUN_TRUST_CLIENT_BASE_URL", "http://trust:9283"),
		ClientID:     getEnv("KUN_TRUST_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_TRUST_CLIENT_SECRET", ""),
	}
	cfg.TrustCallbackSecret = getEnv("KUN_TRUST_CALLBACK_SECRET", "")
	cfg.TrustForwarderClientIDs = splitCSV(getEnv("KUN_TRUST_FORWARDER_CLIENT_IDS", ""))
	cfg.DevPortalClientIDs = splitCSV(getEnv("KUN_DEV_PORTAL_CLIENT_IDS", ""))

	cfg.GalgameImageClient = ImageClientConfig{
		BaseURL:      getEnv("KUN_GALGAME_IMAGE_CLIENT_BASE_URL", cfg.ImageClient.BaseURL),
		ClientID:     getEnv("KUN_GALGAME_IMAGE_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_GALGAME_IMAGE_CLIENT_SECRET", ""),
	}

	cfg.CatalogImageClient = ImageClientConfig{
		BaseURL:      getEnv("KUN_CATALOG_IMAGE_CLIENT_BASE_URL", cfg.ImageClient.BaseURL),
		ClientID:     getEnv("KUN_CATALOG_IMAGE_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_CATALOG_IMAGE_CLIENT_SECRET", ""),
	}

	cfg.NewsImageClient = ImageClientConfig{
		BaseURL:      getEnv("KUN_NEWS_IMAGE_CLIENT_BASE_URL", cfg.ImageClient.BaseURL),
		ClientID:     getEnv("KUN_NEWS_IMAGE_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_NEWS_IMAGE_CLIENT_SECRET", ""),
	}

	cfg.Ymgal = YmgalConfig{
		BaseURL:      getEnv("KUN_YMGAL_BASE_URL", "https://www.ymgal.games"),
		ClientID:     getEnv("KUN_YMGAL_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_YMGAL_CLIENT_SECRET", ""),
	}

	cfg.ArtifactsDatabase = DatabaseConfig{
		Host:     getEnv("KUN_ARTIFACTS_PG_HOST", cfg.Database.Host),
		Port:     getEnv("KUN_ARTIFACTS_PG_PORT", cfg.Database.Port),
		User:     getEnv("KUN_ARTIFACTS_PG_USER", cfg.Database.User),
		Password: getEnv("KUN_ARTIFACTS_PG_PASSWORD", cfg.Database.Password),
		DBName:   getEnv("KUN_ARTIFACTS_PG_DATABASE", "kun_artifacts"),
		SSLMode:  getEnv("KUN_ARTIFACTS_PG_SSLMODE", cfg.Database.SSLMode),
		Timezone: getEnv("KUN_ARTIFACTS_PG_TIMEZONE", cfg.Database.Timezone),
	}

	pool := loadPoolConfig()
	for _, d := range []*DatabaseConfig{
		&cfg.Database, &cfg.GalgameDatabase, &cfg.CatalogDatabase, &cfg.CommunityDatabase,
		&cfg.TrustDatabase, &cfg.AIDatabase, &cfg.NewsDatabase, &cfg.ImagesDatabase,
		&cfg.ArtifactsDatabase,
	} {
		d.Pool = pool
	}

	artifactPathStyle, _ := strconv.ParseBool(getEnv("KUN_ARTIFACT_S3_FORCE_PATH_STYLE", "true"))
	cfg.ArtifactS3 = S3Config{
		Endpoint:        getEnv("KUN_ARTIFACT_S3_ENDPOINT", "http://127.0.0.1:9000"),
		Region:          getEnv("KUN_ARTIFACT_S3_REGION", "us-east-1"),
		AccessKeyID:     getEnv("KUN_ARTIFACT_S3_ACCESS_KEY", ""),
		SecretAccessKey: getEnv("KUN_ARTIFACT_S3_SECRET_KEY", ""),
		Bucket:          getEnv("KUN_ARTIFACT_S3_BUCKET", "kun-artifacts-dev"),
		UsePathStyle:    artifactPathStyle,
	}

	artifactPort, _ := strconv.Atoi(getEnv("KUN_ARTIFACT_PORT", "9279"))
	cfg.ArtifactService = ArtifactServiceConfig{
		Host:             getEnv("KUN_ARTIFACT_HOST", "127.0.0.1"),
		Port:             artifactPort,
		CleanupAccessKey: getEnv("KUN_ARTIFACT_S3_CLEANUP_ACCESS_KEY", ""),
		CleanupSecretKey: getEnv("KUN_ARTIFACT_S3_CLEANUP_SECRET_KEY", ""),
	}

	catalogPort, _ := strconv.Atoi(getEnv("KUN_CATALOG_PORT", "9281"))
	cfg.CatalogService = CatalogServiceConfig{
		Host: getEnv("KUN_CATALOG_HOST", "127.0.0.1"),
		Port: catalogPort,
	}

	communityPort, _ := strconv.Atoi(getEnv("KUN_COMMUNITY_PORT", "9282"))
	cfg.CommunityService = CommunityServiceConfig{
		Host: getEnv("KUN_COMMUNITY_HOST", "127.0.0.1"),
		Port: communityPort,
	}

	trustPort, _ := strconv.Atoi(getEnv("KUN_TRUST_PORT", "9283"))
	cfg.TrustService = TrustServiceConfig{
		Host: getEnv("KUN_TRUST_HOST", "127.0.0.1"),
		Port: trustPort,
	}

	aiPort, _ := strconv.Atoi(getEnv("KUN_AI_PORT", "9284"))
	cfg.AIService = AIServiceConfig{
		Host: getEnv("KUN_AI_HOST", "127.0.0.1"),
		Port: aiPort,
	}
	cfg.AIUpstream = AIUpstreamConfig{
		BaseURL: getEnv("KUN_AI_UPSTREAM_BASE_URL", ""),
		Token:   getEnv("KUN_AI_UPSTREAM_TOKEN", ""),
	}

	cfg.AIOmni = AIOmniConfig{
		BaseURL: getEnv("KUN_AI_OMNI_BASE_URL", "https://api.openai.com"),
		Token:   getEnv("KUN_AI_OMNI_TOKEN", ""),
	}

	cfg.AIClient = AIClientConfig{
		BaseURL:      getEnv("KUN_AI_CLIENT_BASE_URL", "http://127.0.0.1:9284"),
		ClientID:     getEnv("KUN_AI_CLIENT_ID", ""),
		ClientSecret: getEnv("KUN_AI_CLIENT_SECRET", ""),
	}

	cfg.Store = StoreConfig{
		ShortlinkBaseURL: getEnv("KUN_STORE_SHORTLINK_BASE_URL", ""),
		ShortlinkAPIKey:  getEnv("KUN_STORE_SHORTLINK_API_KEY", ""),
		// Deliberately no default. These once fell back to a well-formed URL
		// carrying the affiliate id aid/nextmoe, which does not exist: an
		// unconfigured deployment minted real short links crediting nobody, and
		// a minted alias is pinned to its destination forever. Empty makes the
		// minting face answer 503 instead, which the caller retries.
		AffTemplateManiax: getEnv("KUN_STORE_DLSITE_AFF_URL_TMPL_MANIAX", ""),
		AffTemplatePro:    getEnv("KUN_STORE_DLSITE_AFF_URL_TMPL_PRO", ""),
		PriceDLsiteBase:   getEnv("KUN_STORE_PRICE_DLSITE_BASE", ""),
		PriceDLsiteProxy:  getEnv("KUN_STORE_PRICE_DLSITE_PROXY", ""),
	}

	cfg.NewsModeration = NewsModerationConfig{
		TrustBaseURL: getEnv("KUN_NEWS_TRUST_BASE_URL", cfg.TrustClient.BaseURL),
		AIBaseURL:    getEnv("KUN_NEWS_AI_BASE_URL", cfg.AIClient.BaseURL),
		ClientID:     getEnv("KUN_NEWS_CLIENT_ID", cfg.NewsImageClient.ClientID),
		ClientSecret: getEnv("KUN_NEWS_CLIENT_SECRET", cfg.NewsImageClient.ClientSecret),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Database.Password == "" {
		return fmt.Errorf("KUN_PG_PASSWORD is required")
	}
	if c.JWT.Secret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	return nil
}

func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode, c.Timezone,
	)
}

func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *ServerConfig) IsDevelopment() bool {
	return c.Env == "development"
}

func (c *ServerConfig) IsProduction() bool {
	return c.Env == "production"
}

func (c *Config) ArtifactCleanupS3() S3Config {
	s3 := c.ArtifactS3
	if c.ArtifactService.CleanupAccessKey != "" {
		s3.AccessKeyID = c.ArtifactService.CleanupAccessKey
		s3.SecretAccessKey = c.ArtifactService.CleanupSecretKey
	}
	return s3
}

// MaxIdle defaults to MaxOpen, and derives from it so that raising MaxOpen
// cannot silently re-open the gap. database/sql destroys a returned connection
// the moment the idle count is already at MaxIdle, so a pool of 10 with 5 idle
// slots reconnects on every release above the fifth: catalog opened ~1 new
// backend per second in production (pg_stat_database.sessions 352k over 17h,
// every backend younger than 25s) purely to throw it away again. There is no
// deployment that wants half its own pool discarded on release.
func loadPoolConfig() PoolConfig {
	maxOpen := int(getEnvInt64("KUN_PG_MAX_OPEN_CONNS", 10))
	return PoolConfig{
		MaxOpen:     maxOpen,
		MaxIdle:     int(getEnvInt64("KUN_PG_MAX_IDLE_CONNS", int64(maxOpen))),
		MaxLifetime: time.Duration(getEnvInt64("KUN_PG_CONN_MAX_LIFETIME_SECONDS", 1800)) * time.Second,
		MaxIdleTime: time.Duration(getEnvInt64("KUN_PG_CONN_MAX_IDLE_SECONDS", 300)) * time.Second,
	}
}

// JobPoolConfig bounds a BATCH JOB's pool. Exported because the jobs and the
// one-off cmd/ tools open their own handles rather than going through
// NewPostgresDB, and until they were swept every one of them was the unlimited
// pool the incident above was about — a backfill running beside the services is
// not exempt from the server's finite max_connections just because it is
// short-lived.
//
// Higher than a service's ten because a job's shape is the opposite: a service
// wants a small steady pool serving many short requests, a job wants enough
// slots for its worker fan-out and then exits. Sixteen clears every --workers /
// --concurrency default in the tree (the largest is 16), so bounding the pool
// does not quietly serialize a job that was written to run wide. MaxIdle stays
// low: a job's connections are busy or the job is over.
func JobPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpen:     int(getEnvInt64("KUN_PG_JOB_MAX_OPEN_CONNS", 16)),
		MaxIdle:     int(getEnvInt64("KUN_PG_JOB_MAX_IDLE_CONNS", 4)),
		MaxLifetime: time.Duration(getEnvInt64("KUN_PG_CONN_MAX_LIFETIME_SECONDS", 1800)) * time.Second,
		MaxIdleTime: time.Duration(getEnvInt64("KUN_PG_CONN_MAX_IDLE_SECONDS", 300)) * time.Second,
	}
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return defaultValue
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
