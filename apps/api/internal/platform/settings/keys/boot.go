package keys

import (
	"api/internal/platform/settings"
	"api/pkg/config"
)

var ImageGCColdAfterDays = settings.Int(settings.Meta{
	Name:   "image.gc_cold_after_days",
	DescEN: "Days since an image was last referenced before it becomes a cold-storage candidate.",
	DescZH: "图片距最后一次被引用超过该天数后成为冷存储候选。",
	Min:    settings.F(1),
	Max:    settings.F(3650),
}, 60)

var ImageGCSoftDeleteAfterDays = settings.Int(settings.Meta{
	Name:   "image.gc_softdelete_after_days",
	DescEN: "Days since an image was last referenced before the GC soft-deletes it.",
	DescZH: "图片距最后一次被引用超过该天数后被 GC 软删除。",
	Min:    settings.F(1),
	Max:    settings.F(3650),
}, 365)

var ImageGCHardDeleteAfterDays = settings.Int(settings.Meta{
	Name:   "image.gc_harddelete_after_days",
	DescEN: "Days a soft-deleted image is kept before its bytes are permanently removed.",
	DescZH: "软删除图片在字节被永久清除前的保留天数。",
	Min:    settings.F(1),
	Max:    settings.F(3650),
}, 30)

var ImageGCMaxPerRun = settings.Int(settings.Meta{
	Name:   "image.gc_max_per_run",
	DescEN: "Maximum rows each image GC phase processes per run.",
	DescZH: "图片 GC 每阶段单次运行处理的最大行数。",
	Min:    settings.F(1),
	Max:    settings.F(1000000),
}, 10000)

var ArtifactGCMaxPerRun = settings.Int(settings.Meta{
	Name:   "artifact.gc_max_per_run",
	DescEN: "Maximum rows each artifact GC phase processes per run.",
	DescZH: "文件 GC 每阶段单次运行处理的最大行数。",
	Min:    settings.F(1),
	Max:    settings.F(1000000),
}, 10000)

var StorePriceEnabled = settings.Bool(settings.Meta{
	Name:   "store.price_enabled",
	EnvVar: "KUN_STORE_PRICE_ENABLED",
	DescEN: "Live kill switch for the storefront price face; off answers /v2/store/prices with 503 and pauses background refresh.",
	DescZH: "商店价格面的活杀开关,关闭后 /v2/store/prices 返回 503 并暂停后台刷新。",
}, true)

var StorePriceUserAgent = settings.String(settings.Meta{
	Name:    "store.price_user_agent",
	EnvVar:  "KUN_STORE_PRICE_USER_AGENT",
	DescEN:  "User-Agent header the price fetchers send to storefronts; read live on every request.",
	DescZH:  "价格抓取器请求商店时使用的 User-Agent,每次请求实时读取。",
	Pattern: `^[^\r\n]{1,300}$`,
}, "NextMoe-PriceBot/1.0 (+https://www.kungal.com)")

var StorePriceSteamRegions = settings.StringList(settings.Meta{
	Name:    "store.price_steam_regions",
	EnvVar:  "KUN_STORE_PRICE_STEAM_REGIONS",
	DescEN:  "Steam country codes to quote prices for; applied when the catalog service starts, restart to change.",
	DescZH:  "抓取 Steam 价格的国家码列表,服务启动时生效,修改后需重启 catalog。",
	Pattern: `^[A-Za-z]{2}$`,
}, []string{"jp", "cn", "us"})

var StorePriceDLsiteCurrencies = settings.StringList(settings.Meta{
	Name:    "store.price_dlsite_currencies",
	EnvVar:  "KUN_STORE_PRICE_DLSITE_CURRENCIES",
	DescEN:  "Currencies to keep from DLsite quotes; applied when the catalog service starts, restart to change.",
	DescZH:  "DLsite 报价保留的币种列表,服务启动时生效,修改后需重启 catalog。",
	Pattern: `^[A-Za-z]{3}$`,
}, []string{"CNY", "USD", "TWD", "HKD", "KRW", "EUR"})

var StorePriceWaitOnMissMs = settings.Int(settings.Meta{
	Name:   "store.price_wait_on_miss_ms",
	DescEN: "How long the single-work price endpoint waits for a first fetch on a cache miss; 0 never waits.",
	DescZH: "单作品价格端点在缓存未命中时等待首抓的毫秒数,0 表示不等待。",
	Min:    settings.F(0),
	Max:    settings.F(10000),
}, 1500)

var StorePriceFreshForHours = settings.Int(settings.Meta{
	Name:   "store.price_fresh_for_hours",
	DescEN: "How long a fetched price quote stays fresh before background refresh re-fetches it.",
	DescZH: "已抓取价格报价的保鲜时长,过期后由后台刷新重抓。",
	Min:    settings.F(1),
	Max:    settings.F(168),
}, 6)

var AIUpstreamModel = settings.String(settings.Meta{
	Name:    "ai.upstream_model",
	EnvVar:  "KUN_AI_UPSTREAM_MODEL",
	DescEN:  "Model id the Tier2 LLM lane requests from the upstream gateway; read live on every call.",
	DescZH:  "Tier2 LLM 线向上游网关请求的模型 id,每次调用实时读取。",
	Pattern: `^\S{1,200}$`,
}, config.DefaultAIUpstreamModel)

var AIOmniModel = settings.String(settings.Meta{
	Name:    "ai.omni_model",
	EnvVar:  "KUN_AI_OMNI_MODEL",
	DescEN:  "Model id the Tier1 omni-moderation lane requests; read live on every call.",
	DescZH:  "Tier1 omni 审核线请求的模型 id,每次调用实时读取。",
	Pattern: `^\S{1,200}$`,
}, "omni-moderation-latest")
