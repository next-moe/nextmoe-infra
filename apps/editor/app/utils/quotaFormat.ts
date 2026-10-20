import type { KunUIColor } from '@kungal/ui-core'
import type { EditQuotaCounter } from '#shared/types/quota'

export const quotaCounterLabel = (counter: EditQuotaCounter) => {
  if (counter.limit === null) {
    return `${counter.used} / 不限`
  }
  if (counter.limit === 0) {
    return '不可用'
  }
  return `${counter.used} / ${counter.limit}`
}

export const quotaUsedRatio = (counter: EditQuotaCounter) => {
  if (counter.limit === null || counter.limit === 0) {
    return 0
  }
  return Math.min(counter.used / counter.limit, 1)
}

export const quotaTone = (counter: EditQuotaCounter): KunUIColor => {
  if (counter.limit === 0) {
    return 'default'
  }
  if (counter.remaining === 0) {
    return 'danger'
  }
  return quotaUsedRatio(counter) >= 0.8 ? 'warning' : 'primary'
}

export const quotaRemainingText = (counter: EditQuotaCounter) => {
  if (counter.limit === null) {
    return '不限'
  }
  if (counter.limit === 0) {
    return '—'
  }
  return `剩 ${counter.remaining ?? Math.max(counter.limit - counter.used, 0)}`
}
