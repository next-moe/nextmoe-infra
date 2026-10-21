import type { KunUIColor } from '@kungal/ui-core'

export type DeletionReason =
  | 'not_a_work'
  | 'out_of_scope'
  | 'spam_or_test'
  | 'other'

export const DELETION_REASONS: {
  value: DeletionReason | 'duplicate'
  label: string
  description: string
}[] = [
  {
    value: 'not_a_work',
    label: '不存在的作品',
    description: '查不到任何发售、发布或官方信息，条目本身是编造或误建的'
  },
  {
    value: 'out_of_scope',
    label: '不属于收录范围',
    description: '作品存在，但不是资料库收录的类型（例如不是视觉小说）'
  },
  {
    value: 'spam_or_test',
    label: '恶意或测试条目',
    description: '广告、恶作剧、乱码，或明显用于测试的条目'
  },
  {
    value: 'duplicate',
    label: '和另一个条目重复',
    description: '重复的条目不删除，而是合并到它重复的那一个'
  },
  {
    value: 'other',
    label: '其他原因',
    description: '以上都不符合，在说明里写清楚'
  }
]

export const DELETION_REASON_LABELS: Record<string, string> =
  Object.fromEntries(DELETION_REASONS.map((r) => [r.value, r.label]))

export const DELETION_STATE_LABELS: Record<string, string> = {
  open: '待审核',
  approved: '冷静期',
  executed: '已删除',
  rejected: '已驳回',
  withdrawn: '已撤回',
  restored: '已恢复'
}

export const DELETION_STATE_COLORS: Record<string, KunUIColor> = {
  open: 'warning',
  approved: 'primary',
  executed: 'danger',
  rejected: 'default',
  withdrawn: 'default',
  restored: 'success'
}
