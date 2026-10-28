package keys

import "api/internal/platform/settings"

var AgentEnabled = settings.Bool(settings.Meta{
	Name:       "agent.enabled",
	DescEN:     "Turns the AI assistant on for a site. Off answers every agent request from that site with 403.",
	DescZH:     "为站点开启 AI 助手。关闭时该站点的 agent 请求一律 403。",
	SiteScoped: true,
	Public:     true,
}, false)

var AgentCatalogNSFW = settings.Bool(settings.Meta{
	Name:       "agent.catalog_nsfw",
	DescEN:     "Lets the assistant's catalog tools see r18 works on this site.",
	DescZH:     "允许该站点的助手在资料库工具里看到 r18 作品。",
	SiteScoped: true,
}, false)

var AgentAnonEnabled = settings.Bool(settings.Meta{
	Name:       "agent.anon.enabled",
	DescEN:     "Allows signed-out visitors to use the search mode on this site.",
	DescZH:     "允许未登录访客在该站点使用搜索模式。",
	SiteScoped: true,
	Public:     true,
}, true)

var AgentAnonVerifyMode = settings.Enum(settings.Meta{
	Name:       "agent.anon.verify_mode",
	DescEN:     "Human check a signed-out search must pass: none, or pow (proof of work).",
	DescZH:     "未登录搜索需通过的人机验证:none 不验证,pow 为工作量证明。",
	Enum:       []string{"none", "pow"},
	SiteScoped: true,
	Public:     true,
}, "none")

var AgentAnonRatePerMinute = settings.Int(settings.Meta{
	Name:   "agent.anon.rate_per_minute",
	DescEN: "Signed-out searches allowed per IP address (IPv6 /64) per minute.",
	DescZH: "每个 IP(IPv6 按 /64)每分钟允许的未登录搜索次数。",
	Min:    settings.F(1), Max: settings.F(600),
}, 6)

var AgentAnonQuotaPerDay = settings.Int(settings.Meta{
	Name:   "agent.anon.quota_per_day",
	DescEN: "Signed-out searches allowed per IP address per day.",
	DescZH: "每个 IP 每天允许的未登录搜索次数。",
	Min:    settings.F(1), Max: settings.F(100000),
}, 30)

var AgentAnonGlobalQuotaPerDay = settings.Int(settings.Meta{
	Name:   "agent.anon.global_quota_per_day",
	DescEN: "Ceiling on signed-out searches across the whole platform per day; past it, signed-out search answers 429.",
	DescZH: "全平台每天未登录搜索总量上限,超过后未登录搜索返回 429。",
	Min:    settings.F(1), Max: settings.F(10000000),
}, 50000)

var AgentAnonPowDifficulty = settings.Int(settings.Meta{
	Name:   "agent.anon.pow_difficulty",
	DescEN: "Expected proof-of-work attempts per sub-puzzle for a signed-out search. Linear: doubling it doubles the work.",
	DescZH: "未登录搜索的工作量证明难度(每个子难题的期望尝试次数)。线性:翻倍即工作量翻倍。",
	Min:    settings.F(1), Max: settings.F(1000000),
}, 1000)

var AgentUserRatePerMinute = settings.Int(settings.Meta{
	Name:   "agent.user.rate_per_minute",
	DescEN: "Messages a signed-in user may send to the assistant per minute.",
	DescZH: "登录用户每分钟可向助手发送的消息数。",
	Min:    settings.F(1), Max: settings.F(600),
}, 20)

var AgentFreeUnitsPerDay = settings.Int(settings.Meta{
	Name:   "agent.free.units_per_day",
	DescEN: "Free units each signed-in user gets per day, shared by every site. One unit equals one moemoepoint.",
	DescZH: "每个登录用户每天的免费额度点数,全站共享。1 额度点 = 1 萌萌点。",
	Min:    settings.F(0), Max: settings.F(10000),
}, 20)

var AgentPriceAssistant = settings.Int(settings.Meta{
	Name:   "agent.price.assistant",
	DescEN: "Units one assistant question costs.",
	DescZH: "一次助手提问的价格(额度点)。",
	Min:    settings.F(0), Max: settings.F(100),
}, 1)

var AgentPriceWebSurcharge = settings.Int(settings.Meta{
	Name:   "agent.price.web_surcharge",
	DescEN: "Extra units a question costs when the assistant searched the web for it.",
	DescZH: "助手为该问题联网搜索时额外收取的额度点。",
	Min:    settings.F(0), Max: settings.F(100),
}, 1)

var AgentPriceSearch = settings.Int(settings.Meta{
	Name:   "agent.price.search",
	DescEN: "Units one search-mode query costs a signed-in user.",
	DescZH: "登录用户一次搜索模式查询的价格(额度点)。",
	Min:    settings.F(0), Max: settings.F(100),
}, 0)

var AgentPaidEnabled = settings.Bool(settings.Meta{
	Name:   "agent.paid.enabled",
	DescEN: "Lets a user spend moemoepoints once the day's free units are gone. Off stops at the free units.",
	DescZH: "免费额度用完后允许消耗萌萌点。关闭则免费额度用完即停。",
}, true)

var AgentTurnMaxSteps = settings.Int(settings.Meta{
	Name:   "agent.turn.max_steps",
	DescEN: "Model steps one assistant answer may take; the last one cannot call tools.",
	DescZH: "一次助手回答最多的模型步数,最后一步不能调用工具。",
	Min:    settings.F(1), Max: settings.F(20),
}, 6)

var AgentTurnMaxOutputTokens = settings.Int(settings.Meta{
	Name:   "agent.turn.max_output_tokens",
	DescEN: "Output token cap of each model step in an assistant answer.",
	DescZH: "助手回答每一步的输出 token 上限。",
	Min:    settings.F(64), Max: settings.F(32768),
}, 2048)

var AgentTurnMaxInputChars = settings.Int(settings.Meta{
	Name:   "agent.turn.max_input_chars",
	DescEN: "Longest message, attachments included, a user may send.",
	DescZH: "用户单条消息(含附件)的最大字符数。",
	Min:    settings.F(100), Max: settings.F(200000),
}, 20000)

var AgentContextMaxMessages = settings.Int(settings.Meta{
	Name:   "agent.context.max_messages",
	DescEN: "Most recent messages of the branch sent to the model with each question.",
	DescZH: "每次提问时随附给模型的最近消息条数。",
	Min:    settings.F(2), Max: settings.F(200),
}, 24)

var agentDomain = settings.Domain{
	Name:    "agent",
	TitleZH: "AI 助手",
	Keys: []settings.Entry{
		AgentEnabled, AgentCatalogNSFW,
		AgentAnonEnabled, AgentAnonVerifyMode, AgentAnonRatePerMinute, AgentAnonQuotaPerDay, AgentAnonGlobalQuotaPerDay, AgentAnonPowDifficulty,
		AgentUserRatePerMinute, AgentFreeUnitsPerDay,
		AgentPriceAssistant, AgentPriceWebSurcharge, AgentPriceSearch, AgentPaidEnabled,
		AgentTurnMaxSteps, AgentTurnMaxOutputTokens, AgentTurnMaxInputChars, AgentContextMaxMessages,
	},
}
