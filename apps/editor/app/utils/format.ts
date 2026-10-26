import { format, formatDistanceToNowStrict } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { EDIT_CONFIGS } from '~/constants/editEntities'
import { FIELD_LABELS } from '~/constants/fields'

export const labelOf = (
  map: Record<string, string>,
  key: string | null | undefined
) => (key ? (map[key] ?? key) : '')

const CONFIG_LABELS: Record<string, string> = Object.fromEntries(
  Object.values(EDIT_CONFIGS).flatMap((config) =>
    Object.entries(config.fields).map(([key, field]) => [key, field.label])
  )
)

const MEMBER_LABELS: Record<string, string> = Object.fromEntries(
  Object.values(EDIT_CONFIGS).flatMap((config) =>
    Object.entries(config.fields).flatMap(([key, field]) =>
      Object.entries(field.members ?? {}).flatMap(([member, entry]) =>
        entry.label ? [[`${key}/${member}`, entry.label]] : []
      )
    )
  )
)

export const memberLabel = (fieldKey: string, member: string) =>
  MEMBER_LABELS[`${fieldKey}/${member}`] ?? FIELD_LABELS[member] ?? member

export const fieldLabel = (key: string) => {
  const suppressed = key.endsWith('.suppressed')
  const full = key.replace(/\.suppressed$/, '')
  const bare = full.split('.').pop() ?? full
  const label = CONFIG_LABELS[full] ?? FIELD_LABELS[bare] ?? bare
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
