export const NEWS_FILTER_ALL = '' as const

export const NEWS_STATUS = {
  pending: 0,
  published: 1,
  rejected: 2,
  withdrawn: 3
} as const

export const NEWS_STATUS_LABELS: Record<number, string> = {
  0: '待审',
  1: '已发布',
  2: '已拒绝',
  3: '已撤回'
}

export const NEWS_STATUS_COLORS: Record<
  number,
  'warning' | 'success' | 'danger' | 'default'
> = {
  0: 'warning',
  1: 'success',
  2: 'danger',
  3: 'default'
}

export const NEWS_LANE_LABELS: Record<string, string> = {
  news: '新闻',
  column: '专栏'
}

export const NEWS_SOURCE_LABELS: Record<string, string> = {
  ymgal: '月幕 Galgame',
  galgame_hihyou: 'Galgame 批评'
}

// The transition table mirrors service/admin.go. It is duplicated rather than
// derived because the console has to grey out an illegal action BEFORE the
// request; the server still refuses one with 409, so a drift here shows up as a
// button that fails, never as a write that should not have happened.
export const NEWS_ACTIONS: {
  action: string
  label: string
  icon: string
  color: 'success' | 'danger' | 'warning' | 'default'
  from: number[]
  needsReason?: boolean
}[] = [
  {
    action: 'publish',
    label: '发布',
    icon: 'lucide:send',
    color: 'success',
    from: [NEWS_STATUS.pending, NEWS_STATUS.rejected, NEWS_STATUS.withdrawn]
  },
  {
    action: 'reject',
    label: '拒绝',
    icon: 'lucide:x',
    color: 'danger',
    from: [NEWS_STATUS.pending, NEWS_STATUS.withdrawn],
    needsReason: true
  },
  {
    action: 'withdraw',
    label: '撤回',
    icon: 'lucide:undo-2',
    color: 'warning',
    from: [NEWS_STATUS.published],
    needsReason: true
  },
  {
    action: 'repend',
    label: '退回待审',
    icon: 'lucide:rotate-ccw',
    color: 'default',
    from: [NEWS_STATUS.published, NEWS_STATUS.rejected, NEWS_STATUS.withdrawn]
  }
]

// Mirrors model.ModerationAttemptCeiling. At the ceiling the grading job stops
// re-queueing the item, so "未评分" stops meaning "wait for the next run".
export const NEWS_ATTEMPT_CEILING = 5

export const TIER0_LABELS: Record<string, string> = {
  allow: '放行',
  hold: '存疑',
  deny: '拦截'
}

export const TIER0_COLORS: Record<string, 'success' | 'warning' | 'danger'> = {
  allow: 'success',
  hold: 'warning',
  deny: 'danger'
}

export const NEWS_DEGRADED_LABELS: Record<string, string> = {
  budget: '预算耗尽',
  upstream: '上游报错',
  transport: '连接失败',
  unconfigured: '未配置'
}
