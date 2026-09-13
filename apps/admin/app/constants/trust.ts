
export const TRUST_FILTER_ALL = -1

export type TrustChipColor =
  | 'default'
  | 'primary'
  | 'secondary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'

export const REVIEW_STATUS = {
  pending: 0,
  claimed: 1,
  actioned: 2,
  dismissed: 3
} as const

export const REVIEW_STATUS_LABELS: Record<number, string> = {
  0: '待处理',
  1: '已认领',
  2: '已处置',
  3: '已驳回'
}

export const REVIEW_STATUS_COLORS: Record<number, TrustChipColor> = {
  0: 'warning',
  1: 'info',
  2: 'success',
  3: 'default'
}

export const TERM_PURPOSE = {
  abuse: 0,
  compliance: 1
} as const

export const TERM_PURPOSE_LABELS: Record<number, string> = {
  0: '滥用',
  1: '合规'
}

export const TERM_PURPOSE_COLORS: Record<number, TrustChipColor> = {
  0: 'default',
  1: 'info'
}

export const SCAN_MODE = {
  shadow: 0,
  live: 1
} as const

export const SCAN_MODE_LABELS: Record<number, string> = {
  0: '影子(只记录)',
  1: '执法(开单)'
}

export const SCAN_MODE_COLORS: Record<number, TrustChipColor> = {
  0: 'default',
  1: 'warning'
}

export const REVIEW_SOURCE = {
  reports: 0,
  aiText: 1,
  aiImage: 2,
  communityForward: 3,
  mislabel: 4,
  manual: 5,
  aiSample: 6
} as const

export const REVIEW_SOURCE_LABELS: Record<number, string> = {
  0: '举报',
  1: 'AI 文本',
  2: 'AI 图像',
  3: '社区转交',
  4: '错误标注',
  5: '人工',
  6: '抽检'
}

export const ACTION_LABELS: Record<number, string> = {
  0: '无操作(复核放行)',
  1: '隐藏',
  2: '移除',
  3: '警告用户',
  4: '限制',
  5: '升级至账号中心'
}

export const DECIDE_ACTIONS: ReadonlyArray<number> = [1, 2, 3, 4, 5]

export const REPORT_STATUS_LABELS: Record<number, string> = {
  0: '已接收',
  1: '已关联',
  2: '已折叠'
}

export const REPORT_STATUS_COLORS: Record<number, TrustChipColor> = {
  0: 'warning',
  1: 'info',
  2: 'default'
}

export const CALLBACK_STATUS = {
  pending: 0,
  delivered: 1,
  deadLetter: 2
} as const

export const CALLBACK_STATUS_LABELS: Record<number, string> = {
  0: '待投递',
  1: '已投递',
  2: '死信'
}

export const CALLBACK_STATUS_COLORS: Record<number, TrustChipColor> = {
  0: 'warning',
  1: 'success',
  2: 'danger'
}

export const TERM_KIND = {
  suspect: 0,
  banned: 1
} as const

export const TERM_KIND_LABELS: Record<number, string> = {
  0: '疑似(入队复核)',
  1: '禁用(直接拦截)'
}

export const TERM_KIND_COLORS: Record<number, TrustChipColor> = {
  0: 'warning',
  1: 'danger'
}
