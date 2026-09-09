
export type AiChipColor =
  | 'default'
  | 'primary'
  | 'secondary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'

export const AI_ROUTE_MODERATE_TEXT = 'moderate-text'

export const AI_WINDOW_OPTIONS = [
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' }
] as const

export type AiWindow = (typeof AI_WINDOW_OPTIONS)[number]['value']

export const AI_STATUS_META: Record<
  'ok' | 'upstream_error' | 'truncated' | 'budget_denied' | 'degraded',
  { label: string; color: AiChipColor }
> = {
  ok: { label: '正常', color: 'success' },
  upstream_error: { label: '上游错误', color: 'danger' },
  truncated: { label: '回复被截断', color: 'danger' },
  budget_denied: { label: '超预算', color: 'warning' },
  degraded: { label: '降级', color: 'default' }
}

export const AI_STATUS_KEYS = [
  'ok',
  'upstream_error',
  'truncated',
  'budget_denied',
  'degraded'
] as const

export const AI_DEGRADED_CHANNEL_LABEL = '(降级 · 未拨上游)'
