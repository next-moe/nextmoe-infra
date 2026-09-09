package keys

import "api/internal/platform/settings"

var APIV2DefaultRatePerMinute = settings.Int(settings.Meta{
	Name:   "apiv2.default_rate_per_minute",
	DescEN: "Anonymous and user-token requests allowed per minute before the v2 limiter answers 429.",
	DescZH: "v2 匿名和用户令牌请求每分钟允许的次数,超出后返回 429。",
	Min:    settings.F(1),
	Max:    settings.F(1000000),
}, 100)

var APIV2DefaultQuotaPerDay = settings.Int(settings.Meta{
	Name:   "apiv2.default_quota_per_day",
	DescEN: "Anonymous and user-token requests allowed per UTC day before the v2 limiter answers 429.",
	DescZH: "v2 匿名和用户令牌请求每个 UTC 日允许的次数,超出后返回 429。",
	Min:    settings.F(1),
	Max:    settings.F(100000000),
}, 10000)

var APIV2AuthFailPerMinute = settings.Int(settings.Meta{
	Name:   "apiv2.auth_fail_per_minute",
	DescEN: "Rejected credentials from one IP allowed per minute before that address is blocked.",
	DescZH: "同一 IP 每分钟允许的失败鉴权次数,超出后封锁该地址。",
	Min:    settings.F(1),
	Max:    settings.F(100000),
}, 120)

var APIV2AuthFailBlockSeconds = settings.Int(settings.Meta{
	Name:   "apiv2.auth_fail_block_seconds",
	DescEN: "How long an IP that exceeded the auth-failure budget stays blocked.",
	DescZH: "超出鉴权失败预算的 IP 被封锁的时长。",
	Min:    settings.F(1),
	Max:    settings.F(86400),
}, 60)

var AuthIPRatePerMinute = settings.Int(settings.Meta{
	Name:   "auth.ip_rate_per_minute",
	DescEN: "Unauthenticated requests allowed per IP per minute on the OAuth HTTP surface.",
	DescZH: "OAuth HTTP 面上每个 IP 每分钟允许的未认证请求数。",
	Min:    settings.F(1),
	Max:    settings.F(1000000),
}, 100)

var AuthTokenEndpointRatePerMinute = settings.Int(settings.Meta{
	Name:   "auth.token_endpoint_rate_per_minute",
	DescEN: "Token-endpoint requests allowed per client (or IP) per minute.",
	DescZH: "令牌端点每个客户端(或 IP)每分钟允许的请求数。",
	Min:    settings.F(1),
	Max:    settings.F(1000000),
}, 6000)

var AuthStrictRatePerMinute = settings.Int(settings.Meta{
	Name:   "auth.strict_rate_per_minute",
	DescEN: "Requests allowed per IP per path per minute on strictly limited OAuth routes.",
	DescZH: "严格限流的 OAuth 路由上每个 IP 每个路径每分钟允许的请求数。",
	Min:    settings.F(1),
	Max:    settings.F(100000),
}, 10)

var AuthAllowedEmailDomains = settings.StringList(settings.Meta{
	Name:    "auth.allowed_email_domains",
	DescEN:  "Email domains accepted for registration and email changes.",
	DescZH:  "注册和更换邮箱时接受的邮箱域名列表。",
	Pattern: `^[a-z0-9][a-z0-9.-]*\.[a-z]{2,}$`,
}, []string{
	"qq.com",
	"foxmail.com",
	"163.com",
	"126.com",
	"yeah.net",
	"sina.com",
	"sina.cn",
	"sohu.com",
	"aliyun.com",
	"139.com",
	"189.cn",
	"gmail.com",
	"googlemail.com",
	"outlook.com",
	"hotmail.com",
	"live.com",
	"msn.com",
	"icloud.com",
	"me.com",
	"mac.com",
	"yahoo.com",
	"yahoo.co.jp",
	"proton.me",
	"protonmail.com",
	"pm.me",
})

var AuthFederationProviders = settings.StringList(settings.Meta{
	Name:    "auth.federation_providers",
	DescEN:  "Third-party login providers shown on the login page, in display order. A provider also needs its client credentials configured via env to actually appear.",
	DescZH:  "登录页展示的第三方登录方式（按显示顺序）。还需要通过环境变量配置对应的客户端凭据才会实际生效。",
	Pattern: `^[a-z][a-z0-9_]*$`,
}, []string{})

var AuthVerificationResendCooldownSeconds = settings.Int(settings.Meta{
	Name:   "auth.verification_resend_cooldown_seconds",
	DescEN: "How long a user must wait before requesting another verification code.",
	DescZH: "用户再次请求验证码前必须等待的时间。",
	Min:    settings.F(10),
	Max:    settings.F(3600),
}, 60)

var AuthRegisterGiftPoints = settings.Int(settings.Meta{
	Name:   "auth.register_gift_points",
	DescEN: "Moemoepoints granted to a user on successful registration.",
	DescZH: "用户注册成功时获赠的萌萌点数量。",
	Min:    settings.F(0),
	Max:    settings.F(10000),
}, 7)

var TrustReportRateWindowMinutes = settings.Int(settings.Meta{
	Name:   "trust.report_rate_window_minutes",
	DescEN: "Sliding window over which a reporter's submissions are counted for rate limiting.",
	DescZH: "统计举报人提交次数所用的滑动窗口时长。",
	Min:    settings.F(1),
	Max:    settings.F(1440),
}, 60)

var TrustReportRateMaxPerWindow = settings.Int(settings.Meta{
	Name:   "trust.report_rate_max_per_window",
	DescEN: "Maximum reports one reporter may submit inside the rate-limit window.",
	DescZH: "一个举报人在限流窗口内最多可提交的举报数。",
	Min:    settings.F(1),
	Max:    settings.F(10000),
}, 10)

var TrustAggregateThreshold = settings.Float(settings.Meta{
	Name:   "trust.aggregate_threshold",
	DescEN: "Platform-default report-weight sum at which an open review item is created.",
	DescZH: "创建待审项所需的平台默认举报权重合计阈值。",
	Min:    settings.F(0.1),
	Max:    settings.F(1000),
}, 3.0)

var TrustNewAccountAgeDays = settings.Int(settings.Meta{
	Name:   "trust.new_account_age_days",
	DescEN: "Accounts younger than this receive the reduced reporter weight.",
	DescZH: "注册时间短于该天数的账号使用降低后的举报权重。",
	Min:    settings.F(0),
	Max:    settings.F(365),
}, 7)

var TrustNewAccountReporterWeight = settings.Float(settings.Meta{
	Name:   "trust.new_account_reporter_weight",
	DescEN: "Report weight applied to accounts younger than the new-account age.",
	DescZH: "新账号举报时使用的权重。",
	Min:    settings.F(0),
	Max:    settings.F(1),
}, 0.5)

var TrustPolicyCacheTTLSeconds = settings.Int(settings.Meta{
	Name:   "trust.policy_cache_ttl_seconds",
	DescEN: "How long a process caches per-site Trust & Safety policy rows.",
	DescZH: "进程缓存各站点 Trust & Safety 策略行的时间。",
	Min:    settings.F(1),
	Max:    settings.F(3600),
}, 60)

var TrustTermCacheTTLSeconds = settings.Int(settings.Meta{
	Name:   "trust.term_cache_ttl_seconds",
	DescEN: "How long a process caches the Trust & Safety term list.",
	DescZH: "进程缓存 Trust & Safety 词表的时间。",
	Min:    settings.F(1),
	Max:    settings.F(3600),
}, 60)

// moderateMaxTokens caps the reply. The verdict object itself is ~40 tokens, so
// this looks generous — but it is a CEILING, not a budget: a model that answers
// in 40 tokens is billed for 40 whichever ceiling is set, so raising it costs
// nothing on the replies that already fit and rescues the ones that did not.
//
// It was 256, which silently destroyed half of all escalated verdicts between
// 2026-07-22 and 2026-08-07. Reasoning-style models (the Tier2 channel was
// @cf/zai-org/glm-5.2) emit their working before the JSON; past 256 the object
// was cut mid-token and came back as "unexpected end of JSON input", which
// fail-open turned into an allow. The evidence was unambiguous once looked at:
// successful calls had a median of 132 completion tokens, failed calls a median
// of exactly 256, with 73% pinned to the ceiling.
//
// Tighten this only against measured completion_tokens in ai_usage, never by
// eyeballing the size of the verdict — the verdict is not what fills the budget.
var AIModerateMaxTokens = settings.Int(settings.Meta{
	Name:   "ai.moderate_max_tokens",
	DescEN: "Maximum completion tokens allowed for a Tier2 moderation reply.",
	DescZH: "Tier2 审核回复允许的最大 completion token 数。",
	Min:    settings.F(256),
	Max:    settings.F(32768),
}, 1024)

var CommunitySandboxMaxLinks = settings.Int(settings.Meta{
	Name:   "community.sandbox_max_links",
	DescEN: "Maximum links a sandboxed (TL0) author may put in one post.",
	DescZH: "沙箱(TL0)用户单帖允许的链接数上限。",
	Min:    settings.F(0),
	Max:    settings.F(100),
}, 2)

var CommunitySandboxMaxImages = settings.Int(settings.Meta{
	Name:   "community.sandbox_max_images",
	DescEN: "Maximum images a sandboxed (TL0) author may put in one post.",
	DescZH: "沙箱(TL0)用户单帖允许的图片数上限。",
	Min:    settings.F(0),
	Max:    settings.F(100),
}, 1)

var CommunitySandboxMaxMentions = settings.Int(settings.Meta{
	Name:   "community.sandbox_max_mentions",
	DescEN: "Maximum mentions a sandboxed (TL0) author may put in one post.",
	DescZH: "沙箱(TL0)用户单帖允许的提及数上限。",
	Min:    settings.F(0),
	Max:    settings.F(100),
}, 2)

var CommunitySandboxMaxTopicsPerDay = settings.Int(settings.Meta{
	Name:   "community.sandbox_max_topics_per_day",
	DescEN: "Maximum topics a sandboxed (TL0) author may open inside the sandbox window.",
	DescZH: "沙箱(TL0)用户在沙箱窗口内最多可发的主题数。",
	Min:    settings.F(0),
	Max:    settings.F(1000),
}, 3)

var CommunitySandboxMaxRepliesPerDay = settings.Int(settings.Meta{
	Name:   "community.sandbox_max_replies_per_day",
	DescEN: "Maximum replies a sandboxed (TL0) author may post inside the sandbox window.",
	DescZH: "沙箱(TL0)用户在沙箱窗口内最多可发的回复数。",
	Min:    settings.F(0),
	Max:    settings.F(1000),
}, 10)

var CommunitySandboxWindowHours = settings.Int(settings.Meta{
	Name:   "community.sandbox_window_hours",
	DescEN: "Window over which sandboxed topic and reply counts are measured.",
	DescZH: "统计沙箱用户发主题和回复次数的时间窗口。",
	Min:    settings.F(1),
	Max:    settings.F(168),
}, 24)

var CommunityFlagHideThreshold = settings.Float(settings.Meta{
	Name:   "community.flag_hide_threshold",
	DescEN: "Pending flag-weight sum at which a post is auto-hidden.",
	DescZH: "待处理标记权重合计达到该值时自动隐藏帖子。",
	Min:    settings.F(0.1),
	Max:    settings.F(1000),
}, 3.0)

var CatalogTotalsCacheTTLSeconds = settings.Int(settings.Meta{
	Name:   "catalog.totals_cache_ttl_seconds",
	DescEN: "How long a process caches catalog collection totals.",
	DescZH: "进程缓存 Catalog 集合 total 的时间。",
	Min:    settings.F(1),
	Max:    settings.F(86400),
}, 60)

var CatalogMergeCoolingOffHours = settings.Int(settings.Meta{
	Name:   "catalog.merge_cooling_off_hours",
	DescEN: "Hours an approved merge waits before it may execute; 0 means merges execute immediately.",
	DescZH: "已批准合并在可执行前等待的小时数;0 表示立即执行。",
	Min:    settings.F(0),
	Max:    settings.F(720),
}, 48)

var DeveloperCredentialCacheTTLSeconds = settings.Int(settings.Meta{
	Name:   "developer.credential_cache_ttl_seconds",
	DescEN: "How long a resolved developer API credential stays cached.",
	DescZH: "已解析的开发者 API 凭证在缓存中保留的时间。",
	Min:    settings.F(1),
	Max:    settings.F(3600),
}, 60)

var DeveloperCredentialCacheNegativeTTLSeconds = settings.Int(settings.Meta{
	Name:   "developer.credential_cache_negative_ttl_seconds",
	DescEN: "How long a failed developer API credential lookup stays cached as a miss.",
	DescZH: "开发者 API 凭证查找失败作为未命中缓存的时间。",
	Min:    settings.F(1),
	Max:    settings.F(600),
}, 10)

var apiv2Domain = settings.Domain{
	Name:    "apiv2",
	TitleZH: "开放 API(v2)",
	Keys: []settings.Entry{
		APIV2DefaultRatePerMinute,
		APIV2DefaultQuotaPerDay,
		APIV2AuthFailPerMinute,
		APIV2AuthFailBlockSeconds,
	},
}

var catalogDomain = settings.Domain{
	Name:    "catalog",
	TitleZH: "Catalog 目录",
	Keys: []settings.Entry{
		CatalogTotalsCacheTTLSeconds,
		CatalogMergeCoolingOffHours,
	},
}

var communityDomain = settings.Domain{
	Name:    "community",
	TitleZH: "社区论坛",
	Keys: []settings.Entry{
		CommunitySandboxMaxLinks,
		CommunitySandboxMaxImages,
		CommunitySandboxMaxMentions,
		CommunitySandboxMaxTopicsPerDay,
		CommunitySandboxMaxRepliesPerDay,
		CommunitySandboxWindowHours,
		CommunityFlagHideThreshold,
	},
}

var developerDomain = settings.Domain{
	Name:    "developer",
	TitleZH: "开发者门户",
	Keys: []settings.Entry{
		DeveloperCredentialCacheTTLSeconds,
		DeveloperCredentialCacheNegativeTTLSeconds,
	},
}
