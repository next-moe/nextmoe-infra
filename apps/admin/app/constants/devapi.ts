import type { DevApp, DevKey } from '~~/shared/types/devapi'

export const DEV_TIERS = ['free', 'trusted', 'internal'] as const
export type DevTier = (typeof DEV_TIERS)[number]

export const DEV_TIER_OPTIONS: { value: DevTier; label: string }[] = [
  { value: 'free', label: 'Free（免费）' },
  { value: 'trusted', label: 'Trusted（信任）' },
  { value: 'internal', label: 'Internal（内部，不限流）' },
]

export const DEV_TIER_COLORS: Record<string, 'default' | 'primary' | 'success'> = {
  free: 'default',
  trusted: 'primary',
  internal: 'success',
}

export const DEV_TIER_LIMITS: Record<
  string,
  { rate: number; quota: number; unlimited: boolean }
> = {
  free: { rate: 60, quota: 50_000, unlimited: false },
  trusted: { rate: 600, quota: 1_000_000, unlimited: false },
  internal: { rate: 0, quota: 0, unlimited: true },
}

export const DEV_MINTABLE_SCOPES = ['catalog:read', 'store:read'] as const

// What the mint dialog pre-ticks, kept separate from the mintable set: the
// dialog used to pre-tick everything mintable, so widening that set to
// store:read would have silently handed the scope to every console-minted key.
export const DEV_DEFAULT_SCOPES = ['catalog:read'] as const

// Mirrors devapi's maxAppReviewNoteLen (counted in runes server-side).
export const DEV_APP_REVIEW_NOTE_MAX = 2000

export const DEV_APP_STATUS_TABS = [
  { id: 'enabled', label: '已启用', icon: 'lucide:check' },
  { id: 'pending', label: '待审核', icon: 'lucide:clock' },
  { id: 'declined', label: '已拒绝', icon: 'lucide:x' },
  { id: 'disabled', label: '已停用', icon: 'lucide:power-off' },
  { id: 'archived', label: '已归档', icon: 'lucide:archive' },
  { id: 'all', label: '全部', icon: 'lucide:list' },
]

export const DEV_APP_REVIEW_LABELS: Record<string, string> = {
  approved: '已通过',
  pending: '待审核',
  declined: '已拒绝',
}

export const DEV_APP_REVIEW_COLORS: Record<
  string,
  'success' | 'warning' | 'danger'
> = {
  approved: 'success',
  pending: 'warning',
  declined: 'danger',
}

// Rows written before the approval flow existed carry an empty status and are
// live applications; showing them as "unreviewed" would be a lie.
export const devAppReviewLabel = (status: string): string =>
  DEV_APP_REVIEW_LABELS[status] ?? DEV_APP_REVIEW_LABELS.approved!

export const devAppReviewColor = (
  status: string
): 'success' | 'warning' | 'danger' =>
  DEV_APP_REVIEW_COLORS[status] ?? 'success'

export const DEV_POLICY_MODE_LABELS: Record<string, string> = {
  self_service: '自助',
  approval: '需审批',
  disabled: '已关闭',
}

// Written out rather than composed from the mode name: Tailwind only emits the
// class names it can see as literals in the source.
export const DEV_POLICY_MODE_ACTIVE_CLASS: Record<string, string> = {
  self_service: 'border-success bg-success-50',
  approval: 'border-warning bg-warning-50',
  disabled: 'border-danger bg-danger-50',
}

export const DEV_POLICY_MODE_ICON_CLASS: Record<string, string> = {
  self_service: 'text-success',
  approval: 'text-warning',
  disabled: 'text-danger',
}

export const DEV_POLICY_MODE_HINTS: Record<string, string> = {
  self_service: '用户可以直接完成，无需平台介入',
  approval: '用户提交后进入待审核，由管理台放行',
  disabled: '该功能对所有用户关闭',
}

export const DEV_KEY_STATE_TABS = [
  { id: 'all', label: '全部', icon: 'lucide:list' },
  { id: 'active', label: '活跃', icon: 'lucide:check' },
  { id: 'expired', label: '已过期', icon: 'lucide:clock' },
  { id: 'revoked', label: '已吊销', icon: 'lucide:ban' },
]

export const DEV_KEY_STATE_LABELS: Record<string, string> = {
  active: '活跃',
  expired: '已过期',
  revoked: '已吊销',
}

export const DEV_KEY_STATE_COLORS: Record<
  string,
  'success' | 'default' | 'danger'
> = {
  active: 'success',
  expired: 'default',
  revoked: 'danger',
}

export const DEV_KEY_PAGE_SIZE = 50

// Mirrors apperr.ErrValidationFailed in apps/api/pkg/errors/codes.go: the
// envelope code for a request the API understood and refused. Its message is
// English prose, so the console branches on the code and writes its own Chinese.
export const API_CODE_VALIDATION_FAILED = 7

// A refused delete is explained from the row the console already holds, never
// from res.message. The buttons only render for a row that looks deletable, so
// a stale list is exactly the case that reaches here — say so and refresh
// rather than guess. Anything that is not the guard keeps the server's own
// message, so a genuine failure is never mislabelled as "not deletable".
export const devKeyDeleteMessage = (
  key: DevKey,
  code: number,
  fallback: string
): string => {
  if (code !== API_CODE_VALIDATION_FAILED) return fallback || '删除失败'
  if (key.last_used_at) return '该密钥已产生调用记录，只能吊销，不能删除'
  if (!key.revoked_at) return '请先吊销该密钥，再删除'
  return '当前不可删除，已为你刷新密钥列表'
}

// Only the clearable reasons are named here: login_client and bound_site
// withhold the button entirely, so a refusal that reaches a click is always
// something an operator can actually go and clear.
export const devAppDeleteMessage = (
  app: DevApp | null,
  code: number,
  fallback: string
): string => {
  if (code !== API_CODE_VALIDATION_FAILED) return fallback || '删除失败'
  if (!app?.archived_at) return '请先归档该应用，再删除'
  return '该应用还有密钥、调用记录、商店短链或未过期的登录会话挂在名下，先清理掉再删，或者就让它一直归档着'
}

// Neither flag can ever be cleared, which is why the button is withheld rather
// than offered: cleanup hard-deletes sessions and authorization codes, so "no
// logins right now" is no evidence of what a client once did, and the rows it
// left behind reach into a database the delete guard cannot see.
export const devAppDeleteBlockedReason = (app: DevApp | null): string => {
  if (app?.login_client) return '可用于用户登录，不可删除'
  if (app?.bound_site) return '已绑定站点，不可删除'
  return ''
}

export const devTierLimitHint = (tier: string): string => {
  const l = DEV_TIER_LIMITS[tier]
  if (!l) return ''
  if (l.unlimited) return '不限流 / 不限配额'
  return `${l.rate} 次/分 · ${l.quota.toLocaleString()} 次/日`
}
