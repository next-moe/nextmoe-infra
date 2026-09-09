package keys

import "api/internal/platform/settings"

var PlatformReadOnly = settings.Bool(settings.Meta{
	Name:       "platform.read_only",
	DescEN:     "Tells every site to refuse writes (posting, editing, uploads) and show a maintenance notice; used during platform-wide maintenance such as a database move.",
	DescZH:     "通知所有站点拒绝写操作(发帖/编辑/上传)并展示维护提示,用于数据库迁移等全平台维护窗口。",
	SiteScoped: true,
	Public:     true,
}, false)

var PlatformNotice = settings.String(settings.Meta{
	Name:       "platform.notice",
	DescEN:     "A one-line notice every site should show at the top of its pages; empty shows nothing.",
	DescZH:     "各站点应在页面顶部展示的一行公告,留空则不展示。",
	Pattern:    `^[^\n]{0,500}$`,
	SiteScoped: true,
	Public:     true,
}, "")

var AuthVerificationCodeTTLMinutes = settings.Int(settings.Meta{
	Name:   "auth.verification_code_ttl_minutes",
	EnvVar: "KUN_AUTH_VERIFICATION_CODE_TTL_MINUTES",
	DescEN: "How long a login or email-change verification code stays valid before the user must request a new one.",
	DescZH: "登录或更换邮箱验证码在过期前的有效时间,超时需重新获取。",
	Min:    settings.F(1),
	Max:    settings.F(1440),
}, 15)

var ImageUploadEnabled = settings.Bool(settings.Meta{
	Name:       "image.upload_enabled",
	EnvVar:     "KUN_IMAGE_UPLOAD_ENABLED",
	DescEN:     "Master switch for accepting new image uploads; off rejects every upload.",
	DescZH:     "图床上传总开关,关闭后拒绝所有新上传。",
	SiteScoped: true,
	Public:     true,
}, false)

var ArtifactUploadEnabled = settings.Bool(settings.Meta{
	Name:       "artifact.upload_enabled",
	EnvVar:     "KUN_ARTIFACT_UPLOAD_ENABLED",
	DescEN:     "Master switch for accepting new artifact uploads; off rejects every upload.",
	DescZH:     "文件存储上传总开关,关闭后拒绝所有新上传。",
	SiteScoped: true,
	Public:     true,
}, false)

var ArtifactMultipartThresholdBytes = settings.Int(settings.Meta{
	Name:   "artifact.multipart_threshold_bytes",
	EnvVar: "KUN_ARTIFACT_MULTIPART_THRESHOLD",
	DescEN: "Files larger than this are uploaded with multipart presign instead of a single PUT.",
	DescZH: "超过该大小的文件改走分片预签名上传,而不是单次 PUT。",
	Min:    settings.F(1048576),
	Max:    settings.F(5368709120),
}, 52428800)

var ArtifactPartSizeBytes = settings.Int(settings.Meta{
	Name:   "artifact.part_size_bytes",
	EnvVar: "KUN_ARTIFACT_PART_SIZE",
	DescEN: "Size of each part in a multipart artifact upload.",
	DescZH: "分片上传时每个分片的大小。",
	Min:    settings.F(5242880),
	Max:    settings.F(5368709120),
}, 16777216)

var ArtifactPresignUploadTTLSeconds = settings.Int(settings.Meta{
	Name:   "artifact.presign_upload_ttl_seconds",
	EnvVar: "KUN_ARTIFACT_PRESIGN_UPLOAD_TTL_SECONDS",
	DescEN: "How long a presigned artifact upload URL remains valid.",
	DescZH: "文件上传预签名 URL 的有效时间。",
	Min:    settings.F(60),
	Max:    settings.F(604800),
}, 3600)

var ArtifactPresignDownloadTTLSeconds = settings.Int(settings.Meta{
	Name:   "artifact.presign_download_ttl_seconds",
	EnvVar: "KUN_ARTIFACT_PRESIGN_DOWNLOAD_TTL_SECONDS",
	DescEN: "How long a presigned artifact download URL remains valid.",
	DescZH: "文件下载预签名 URL 的有效时间。",
	Min:    settings.F(60),
	Max:    settings.F(604800),
}, 86400)

var ArtifactOrphanTTLHours = settings.Int(settings.Meta{
	Name:   "artifact.orphan_ttl_hours",
	EnvVar: "KUN_ARTIFACT_ORPHAN_TTL_HOURS",
	DescEN: "How long an uploaded artifact may sit uncommitted before it is reclaimed as an orphan.",
	DescZH: "已上传但未提交的文件在被当作孤儿回收前的保留时间。",
	Min:    settings.F(1),
	Max:    settings.F(8760),
}, 24)

var ArtifactSoftDeleteTTLHours = settings.Int(settings.Meta{
	Name:   "artifact.softdelete_ttl_hours",
	EnvVar: "KUN_ARTIFACT_SOFTDELETE_TTL_HOURS",
	DescEN: "How long a soft-deleted artifact is kept before it is permanently removed.",
	DescZH: "软删除文件在被永久清除前的保留时间。",
	Min:    settings.F(1),
	Max:    settings.F(8760),
}, 168)

var ArtifactReclaimMinIdleSeconds = settings.Int(settings.Meta{
	Name:   "artifact.reclaim_min_idle_seconds",
	EnvVar: "KUN_ARTIFACT_RECLAIM_MIN_IDLE_SECONDS",
	DescEN: "How long an artifact stuck in the uploading state must have been idle before the console may reclaim it.",
	DescZH: "卡在上传中状态的文件必须空闲多久,控制台才允许回收它。",
	Min:    settings.F(60),
	Max:    settings.F(604800),
}, 3600)

var TrustScanEnabled = settings.Bool(settings.Meta{
	Name:   "trust.scan_enabled",
	EnvVar: "KUN_TRUST_SCAN_ENABLED",
	DescEN: "Whether community sends new content to Trust & Safety for asynchronous scanning.",
	DescZH: "community 是否把新内容送往 Trust & Safety 做异步扫描。",
}, false)

var TrustCheckEnabled = settings.Bool(settings.Meta{
	Name:   "trust.check_enabled",
	EnvVar: "KUN_TRUST_CHECK_ENABLED",
	DescEN: "Whether community runs the synchronous Trust & Safety word-list check before accepting new content.",
	DescZH: "community 接受新内容前是否同步过 Trust & Safety 词表门。",
}, false)

var TrustScanMode = settings.Enum(settings.Meta{
	Name:   "trust.scan_mode",
	EnvVar: "KUN_TRUST_SCAN_MODE",
	DescEN: "Shadow records scanner verdicts without acting; live enforces them.",
	DescZH: "shadow 只记录扫描结论不处置,live 按结论执行处置。",
	Enum:   []string{"shadow", "live"},
}, "shadow")

var TrustScanSampleRate = settings.Float(settings.Meta{
	Name:   "trust.scan_sample_rate",
	EnvVar: "KUN_TRUST_SCAN_SAMPLE_RATE",
	DescEN: "Fraction of eligible traffic the scanner actually scores, capped at 5 percent.",
	DescZH: "扫描器实际打分的流量比例,上限 5%。",
	Min:    settings.F(0),
	Max:    settings.F(0.05),
}, 0)

var AIEscalateThreshold = settings.Float(settings.Meta{
	Name:   "ai.escalate_threshold",
	EnvVar: "KUN_AI_ESCALATE_THRESHOLD",
	DescEN: "Tier1 omni-moderation score at or above which a request escalates to the Tier2 LLM.",
	DescZH: "Tier1 omni 评分达到或超过该值时升级到 Tier2 LLM。",
	Min:    settings.F(0),
	Max:    settings.F(1),
}, 0.4)

var AINegativeSampleRate = settings.Float(settings.Meta{
	Name:   "ai.negative_sample_rate",
	EnvVar: "KUN_AI_NEGATIVE_SAMPLE_RATE",
	DescEN: "Fraction of below-threshold traffic that is still sampled for review.",
	DescZH: "未达阈值的流量中仍被抽样送审的比例。",
	Min:    settings.F(0),
	Max:    settings.F(1),
}, 0.05)

var AIForceEscalate = settings.StringList(settings.Meta{
	Name:    "ai.force_escalate",
	EnvVar:  "KUN_AI_FORCE_ESCALATE",
	DescEN:  "Site:kind pairs that always escalate, skipping the score threshold.",
	DescZH:  "始终升级、绕过分数阈值的 site:kind 对。",
	Pattern: `^[A-Za-z0-9_-]+:[A-Za-z0-9_-]+$`,
}, []string{})

var StoreLinkQuotaPerClient = settings.Int(settings.Meta{
	Name:   "store.link_quota_per_client",
	EnvVar: "KUN_STORE_LINK_QUOTA_PER_CLIENT",
	DescEN: "Maximum number of distinct DLsite products one calling site may mint short links for.",
	DescZH: "单个调用站可铸造短链的 DLsite 商品数上限。",
	Min:    settings.F(1),
	Max:    settings.F(1000000),
}, 5000)

var live = settings.NewRegistry(
	settings.Domain{
		Name:    "platform",
		TitleZH: "平台策略",
		Keys:    []settings.Entry{PlatformReadOnly, PlatformNotice},
	},
	settings.Domain{
		Name:    "auth",
		TitleZH: "账号与验证",
		Keys: []settings.Entry{
			AuthVerificationCodeTTLMinutes,
			AuthIPRatePerMinute,
			AuthTokenEndpointRatePerMinute,
			AuthStrictRatePerMinute,
			AuthAllowedEmailDomains,
			AuthFederationProviders,
			AuthVerificationResendCooldownSeconds,
			AuthRegisterGiftPoints,
		},
	},
	settings.Domain{
		Name:    "image",
		TitleZH: "图床",
		Keys: []settings.Entry{
			ImageUploadEnabled,
			ImageGCColdAfterDays,
			ImageGCSoftDeleteAfterDays,
			ImageGCHardDeleteAfterDays,
			ImageGCMaxPerRun,
		},
	},
	settings.Domain{
		Name:    "artifact",
		TitleZH: "文件存储",
		Keys: []settings.Entry{
			ArtifactUploadEnabled,
			ArtifactMultipartThresholdBytes,
			ArtifactPartSizeBytes,
			ArtifactPresignUploadTTLSeconds,
			ArtifactPresignDownloadTTLSeconds,
			ArtifactOrphanTTLHours,
			ArtifactSoftDeleteTTLHours,
			ArtifactReclaimMinIdleSeconds,
			ArtifactGCMaxPerRun,
		},
	},
	settings.Domain{
		Name:    "trust",
		TitleZH: "Trust & Safety",
		Keys: []settings.Entry{
			TrustScanEnabled,
			TrustCheckEnabled,
			TrustScanMode,
			TrustScanSampleRate,
			TrustReportRateWindowMinutes,
			TrustReportRateMaxPerWindow,
			TrustAggregateThreshold,
			TrustNewAccountAgeDays,
			TrustNewAccountReporterWeight,
			TrustPolicyCacheTTLSeconds,
			TrustTermCacheTTLSeconds,
		},
	},
	settings.Domain{
		Name:    "ai",
		TitleZH: "AI 网关",
		Keys: []settings.Entry{
			AIEscalateThreshold,
			AINegativeSampleRate,
			AIForceEscalate,
			AIModerateMaxTokens,
			AIUpstreamModel,
			AIOmniModel,
		},
	},
	settings.Domain{
		Name:    "store",
		TitleZH: "商店与分销",
		Keys: []settings.Entry{
			StoreLinkQuotaPerClient,
			StorePriceEnabled,
			StorePriceUserAgent,
			StorePriceSteamRegions,
			StorePriceDLsiteCurrencies,
			StorePriceWaitOnMissMs,
			StorePriceFreshForHours,
		},
	},
	apiv2Domain,
	catalogDomain,
	communityDomain,
	developerDomain,
	jobsDomain,
)

func Live() *settings.Registry { return live }
