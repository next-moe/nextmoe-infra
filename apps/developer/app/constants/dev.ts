export const DEV_TIER_COLORS: Record<
  string,
  'default' | 'primary' | 'success'
> = {
  free: 'default',
  trusted: 'primary',
  internal: 'success'
}

export const DEV_TIER_LABELS: Record<string, string> = {
  free: 'Free（免费）',
  trusted: 'Trusted（信任）',
  internal: 'Internal（内部）'
}

export const DEV_MINTABLE_SCOPES = [
  'catalog:read',
  'store:read',
  'moyu:read',
  'sticker:read'
] as const

// What a fresh mint dialog pre-ticks. store:read is mintable but deliberately
// not defaulted: it opens per-site link minting with a per-app product cap, so
// holding it should be the caller's explicit choice.
export const DEV_DEFAULT_SCOPES = ['catalog:read'] as const

export const DEV_CAP_APP_CREATE = 'app.create'
export const DEV_CAP_APP_MANAGE = 'app.manage'
export const DEV_CAP_KEY_MINT = 'key.mint'

// A row written before the approval flow existed carries an empty status and is
// a live application, so the portal must read it as approved rather than as an
// unknown state.
export const DEV_APP_REVIEW_LABELS: Record<string, string> = {
  approved: '已通过',
  pending: '待审核',
  declined: '未通过'
}

export const DEV_APP_REVIEW_COLORS: Record<
  string,
  'success' | 'warning' | 'danger'
> = {
  approved: 'success',
  pending: 'warning',
  declined: 'danger'
}

export const isAppUnderReview = (status: string): boolean =>
  status === 'pending' || status === 'declined'

// Mirrors apperr.ErrValidationFailed in apps/api/pkg/errors/codes.go: the
// envelope code for a request the API understood and refused. Its message is
// English prose, so the portal branches on the code and writes its own Chinese.
export const API_CODE_VALIDATION_FAILED = 7

export const DEV_DISABLED_HINT = '该功能当前已由平台关闭'

export const MAX_APPS_PER_ACCOUNT = 5

export const DEV_USAGE_DAY_OPTIONS = [7, 14, 30]
export const MAX_ACTIVE_KEYS_PER_APP = 5

export const API_BASE_URL = 'https://api.nextmoe.dev/v2'

export const MCP_ENDPOINT = 'https://mcp.nextmoe.dev/mcp'
