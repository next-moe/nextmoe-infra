import { format, formatDistanceToNowStrict } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { FIELD_LABELS } from '~/constants/fields'

export const labelOf = (
  map: Record<string, string>,
  key: string | null | undefined
) => (key ? (map[key] ?? key) : '')

export const fieldLabel = (key: string) => {
  const suppressed = key.endsWith('.suppressed')
  const bare =
    key
      .replace(/\.suppressed$/, '')
      .split('.')
      .pop() ?? key
  const label = FIELD_LABELS[bare] ?? bare
  return suppressed ? `${label}(屏蔽)` : label
}

export const formatDateTime = (iso: string | null | undefined) =>
  iso ? format(new Date(iso), 'yyyy-MM-dd HH:mm') : ''

export const formatRelative = (iso: string | null | undefined) =>
  iso
    ? formatDistanceToNowStrict(new Date(iso), {
        addSuffix: true,
        locale: zhCN
      })
    : ''

export const formatReleaseDate = (
  date: string | null | undefined,
  precision: string | null | undefined
) => {
  if (!date) {
    return ''
  }
  if (precision === 'year') {
    return date.slice(0, 4)
  }
  if (precision === 'month') {
    return date.slice(0, 7)
  }
  return date
}
