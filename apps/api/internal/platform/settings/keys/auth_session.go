package keys

import "api/internal/platform/settings"

var AuthSSOSessionIdleDays = settings.Int(settings.Meta{
	Name:   "auth.sso_session_idle_days",
	DescEN: "How long the account center keeps a browser signed in with no activity. Any app the user keeps using in that browser counts as activity, so silent sign-in into another app keeps working.",
	DescZH: "账号中心在浏览器无任何活动时保持登录的天数。用户在该浏览器继续使用任一应用都算活动,从而进入其他应用时仍可无感登录。",
	Min:    settings.F(1),
	Max:    settings.F(365),
}, 30)

var AuthSSOSessionMaxDays = settings.Int(settings.Meta{
	Name:   "auth.sso_session_max_days",
	DescEN: "Hard limit on an account-center sign-in counted from the moment the user signed in; activity never extends it past this.",
	DescZH: "账号中心登录的绝对上限天数,从用户登录那一刻起算;再多活动也不能延长超过这个期限。",
	Min:    settings.F(1),
	Max:    settings.F(730),
}, 365)

var AuthCreatorApplicationCooldownHours = settings.Int(settings.Meta{
	Name:   "auth.creator_application_cooldown_hours",
	DescEN: "How long a user must wait after a declined creator application before applying again.",
	DescZH: "创作者申请被拒绝后,用户需要等待多少小时才能再次申请。",
	Min:    settings.F(0),
	Max:    settings.F(8760),
}, 24)

var AuthEditorApplicationCooldownHours = settings.Int(settings.Meta{
	Name:   "auth.editor_application_cooldown_hours",
	DescEN: "How long a user must wait after a declined editor application before applying again.",
	DescZH: "编辑者申请被拒绝后,用户需要等待多少小时才能再次申请。",
	Min:    settings.F(0),
	Max:    settings.F(8760),
}, 24)
