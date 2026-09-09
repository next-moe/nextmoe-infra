package errors

const (
	ErrBadRequest       = 1
	ErrInvalidID        = 2
	ErrInternalServer   = 3
	ErrNotFound         = 4
	ErrForbidden        = 5
	ErrTooManyRequests  = 6
	ErrValidationFailed = 7
	ErrMissingParam     = 8
	ErrInvalidParam     = 9
	ErrOperationFailed  = 10
	ErrGone             = 11
	ErrMoved            = 12

	ErrAuthUnauthorized           = 10001
	ErrAuthInvalidToken           = 10002
	ErrAuthTokenExpired           = 10003
	ErrAuthInvalidPassword        = 10004
	ErrAuthUserNotFound           = 10005
	ErrAuthEmailExists            = 10006
	ErrAuthNameExists             = 10007
	ErrAuthPasswordRequired       = 10008
	ErrAuthInvalidEmail           = 10009
	ErrAuthCodeInvalid            = 10010
	ErrAuthCodeExpired            = 10011
	ErrAuthEmailChangeTooFrequent = 10012
	ErrAuthEmailSameAsCurrent     = 10013
	ErrAuthUserBanned             = 10014
	ErrAuthEmailDomainNotAllowed  = 10015
	ErrAuthStepUpRequired         = 10016
	ErrAuthFederationDisabled     = 10017
	ErrAuthFederationExpired      = 10018
	ErrAuthFederationConflict     = 10019

	ErrOAuthInvalidClient        = 15001
	ErrOAuthInvalidRedirectURI   = 15002
	ErrOAuthInvalidCode          = 15003
	ErrOAuthInvalidCodeVerifier  = 15004
	ErrOAuthInvalidGrant         = 15005
	ErrOAuthInvalidScope         = 15006
	ErrOAuthAccessDenied         = 15007
	ErrOAuthInvalidClientSecret  = 15008
	ErrOAuthPKCERequired         = 15009
	ErrOAuthConsentRequired      = 15010
	ErrOAuthUnsupportedGrantType = 15011

	ErrMoemoepointInvalidDelta  = 16002
	ErrMoemoepointInvalidReason = 16003
	ErrMoemoepointIdemConflict  = 16004
	ErrMoemoepointNotAwarder    = 16005

	ErrCreatorAlreadyHas    = 17001
	ErrCreatorAppPending    = 17002
	ErrCreatorAppCooldown   = 17003
	ErrCreatorAppNotFound   = 17004
	ErrCreatorAppNotPending = 17005

	ErrGalgameNotFound           = 20001
	ErrGalgameAlreadyExists      = 20002
	ErrGalgameInvalidVNDB        = 20003
	ErrGalgameVNDBExists         = 20004
	ErrGalgameForbidden          = 20005
	ErrGalgameClaimNotDraft      = 20006
	ErrGalgameSubmitterOnly      = 20007
	ErrGalgameDraftStatusInvalid = 20008
	ErrGalgameQuotaExceeded      = 20009

	ErrArtifactNotFound         = 50001
	ErrArtifactInvalid          = 50002
	ErrArtifactVirusFound       = 50003
	ErrArtifactTooBig           = 50004
	ErrArtifactProcessing       = 50005
	ErrArtifactUnauthorized     = 50006
	ErrArtifactBadClient        = 50007
	ErrArtifactBadSecret        = 50008
	ErrArtifactSiteDisabled     = 50009
	ErrArtifactSiteUnconfigured = 50010
	ErrArtifactBadRequest       = 50011
	ErrArtifactQuotaExceeded    = 50012
	ErrArtifactStoreFailed      = 50013
	ErrArtifactUploadDisabled   = 50014
	ErrArtifactSizeMismatch     = 50015
	ErrArtifactForbidden        = 50016
	ErrArtifactMIMEDenied       = 50017

	ErrModerationPending  = 60001
	ErrModerationRejected = 60002

	ErrSiteNotFound      = 70001
	ErrSiteAlreadyExists = 70002

	ErrImageUnauthorized     = 80001
	ErrImageBadClient        = 80002
	ErrImageBadSecret        = 80003
	ErrImageSiteDisabled     = 80004
	ErrImageSiteUnconfigured = 80005
	ErrImagePresetDenied     = 80006
	ErrImageFileTooLarge     = 80007
	ErrImageQuotaExceeded    = 80008
	ErrImageMIMEDenied       = 80009
	ErrImageDecodeFailed     = 80010
	ErrImagePresetNotFound   = 80011
	ErrImageStoreFailed      = 80012
	ErrImageNotFound         = 80013
	ErrImageBadRequest       = 80014
	ErrImageUploadDisabled   = 80015

	ErrStoreInvalidProduct  = 90001
	ErrStoreQuotaExceeded   = 90002
	ErrStoreLinkUnavailable = 90003
	ErrStoreUnconfigured    = 90004
)

var codeMessages = map[int]string{
	ErrBadRequest:       "请求格式错误",
	ErrInvalidID:        "无效的 ID",
	ErrInternalServer:   "服务器内部错误",
	ErrNotFound:         "资源不存在",
	ErrForbidden:        "访问被拒绝",
	ErrTooManyRequests:  "请求过于频繁",
	ErrValidationFailed: "参数验证失败",
	ErrMissingParam:     "缺少必要参数",
	ErrInvalidParam:     "参数格式错误",
	ErrOperationFailed:  "操作失败",
	ErrGone:             "接口已下线",
	ErrMoved:            "资源已合并",

	ErrAuthUnauthorized:           "未授权，请先登录",
	ErrAuthInvalidToken:           "无效的令牌",
	ErrAuthTokenExpired:           "令牌已过期，请重新登录",
	ErrAuthInvalidPassword:        "邮箱或密码错误",
	ErrAuthUserNotFound:           "用户不存在",
	ErrAuthEmailExists:            "该邮箱已被注册",
	ErrAuthNameExists:             "该用户名已被使用",
	ErrAuthPasswordRequired:       "需要重置密码",
	ErrAuthInvalidEmail:           "邮箱格式不正确",
	ErrAuthCodeInvalid:            "验证码无效",
	ErrAuthCodeExpired:            "验证码已过期",
	ErrAuthEmailChangeTooFrequent: "邮箱验证码发送过于频繁，请稍后再试",
	ErrAuthEmailSameAsCurrent:     "新邮箱与当前邮箱相同",
	ErrAuthUserBanned:             "账号已被封禁",
	ErrAuthEmailDomainNotAllowed:  "该邮箱服务商暂不支持，请使用 QQ、网易、Gmail、Outlook、iCloud 等常见邮箱",
	ErrAuthStepUpRequired:         "切换到该账号需要重新验证身份",
	ErrAuthFederationDisabled:     "该第三方登录方式未启用",
	ErrAuthFederationExpired:      "第三方登录已过期，请重新发起",
	ErrAuthFederationConflict:     "该第三方账号的绑定关系存在冲突，请使用密码登录",

	ErrCreatorAlreadyHas:    "你已经是创作者了",
	ErrCreatorAppPending:    "已有一份待审核的创作者申请",
	ErrCreatorAppCooldown:   "申请被拒绝后需等待冷却期才能重新申请",
	ErrCreatorAppNotFound:   "申请不存在",
	ErrCreatorAppNotPending: "该申请已被处理",

	ErrOAuthInvalidClient:        "无效的客户端",
	ErrOAuthInvalidRedirectURI:   "无效的回调地址",
	ErrOAuthInvalidCode:          "无效的授权码",
	ErrOAuthInvalidCodeVerifier:  "无效的代码验证器",
	ErrOAuthInvalidGrant:         "客户端未被授权使用该授权类型",
	ErrOAuthInvalidScope:         "无效的权限范围",
	ErrOAuthAccessDenied:         "访问被拒绝",
	ErrOAuthInvalidClientSecret:  "客户端密钥无效",
	ErrOAuthPKCERequired:         "公开客户端必须使用 PKCE",
	ErrOAuthConsentRequired:      "需要用户授权同意",
	ErrOAuthUnsupportedGrantType: "不支持的授权类型",

	ErrMoemoepointInvalidDelta:  "萌萌点变动值不能为 0",
	ErrMoemoepointInvalidReason: "未知的萌萌点变动原因",
	ErrMoemoepointIdemConflict:  "幂等键已存在但请求内容不一致",
	ErrMoemoepointNotAwarder:    "该客户端无权发放萌萌点",

	ErrGalgameNotFound:           "Galgame 不存在",
	ErrGalgameAlreadyExists:      "Galgame 已存在",
	ErrGalgameInvalidVNDB:        "无效的 VNDB ID",
	ErrGalgameVNDBExists:         "该 VNDB ID 的 Galgame 已存在",
	ErrGalgameForbidden:          "无权操作此 Galgame",
	ErrGalgameClaimNotDraft:      "草稿不可认领",
	ErrGalgameSubmitterOnly:      "仅提交者可编辑",
	ErrGalgameDraftStatusInvalid: "草稿仅可在待审/已拒状态编辑",
	ErrGalgameQuotaExceeded:      "今日投稿配额已用尽",

	ErrArtifactNotFound:         "资源不存在",
	ErrArtifactInvalid:          "无效的资源",
	ErrArtifactVirusFound:       "检测到病毒文件",
	ErrArtifactTooBig:           "文件过大",
	ErrArtifactProcessing:       "资源正在处理中",
	ErrArtifactUnauthorized:     "未授权访问制品服务",
	ErrArtifactBadClient:        "无效的客户端 ID",
	ErrArtifactBadSecret:        "客户端密钥错误",
	ErrArtifactSiteDisabled:     "该站点未开启制品服务",
	ErrArtifactSiteUnconfigured: "该站点缺少制品服务配置",
	ErrArtifactBadRequest:       "请求格式错误",
	ErrArtifactQuotaExceeded:    "当日配额已用完",
	ErrArtifactStoreFailed:      "制品存储失败",
	ErrArtifactUploadDisabled:   "制品上传功能暂未开放，敬请期待",
	ErrArtifactSizeMismatch:     "上传文件大小与声明不符",
	ErrArtifactForbidden:        "无权操作此制品",
	ErrArtifactMIMEDenied:       "该站点不接受此文件类型",

	ErrModerationPending:  "内容待审核",
	ErrModerationRejected: "内容审核未通过",

	ErrSiteNotFound:      "站点不存在",
	ErrSiteAlreadyExists: "站点已存在",

	ErrImageUnauthorized:     "未授权访问图片服务",
	ErrImageBadClient:        "无效的客户端 ID",
	ErrImageBadSecret:        "客户端密钥错误",
	ErrImageSiteDisabled:     "该站点未开启图片服务",
	ErrImageSiteUnconfigured: "该站点缺少图片服务配置",
	ErrImagePresetDenied:     "该站点不允许使用此 preset",
	ErrImageFileTooLarge:     "文件大小超过限制",
	ErrImageQuotaExceeded:    "当日配额已用完",
	ErrImageMIMEDenied:       "该 preset 不接受此格式",
	ErrImageDecodeFailed:     "图片解码失败",
	ErrImagePresetNotFound:   "未知的 preset",
	ErrImageStoreFailed:      "图片存储失败",
	ErrImageNotFound:         "图片不存在",
	ErrImageBadRequest:       "请求格式错误",
	ErrImageUploadDisabled:   "图片上传功能暂未开放，敬请期待",

	ErrStoreInvalidProduct:  "无效的 DLsite 商品号",
	ErrStoreQuotaExceeded:   "该应用的分销链接配额已用尽",
	ErrStoreLinkUnavailable: "短链服务暂不可用，请稍后重试",
	ErrStoreUnconfigured:    "分销链接服务未配置",
}

func GetMessage(code int) string {
	if msg, ok := codeMessages[code]; ok {
		return msg
	}
	return "未知错误"
}
