import type { CatalogEditContextRow } from '#shared/types/catalog'
import { WORK_RELATION_PHRASES } from '~/constants/related'
import { rowIdentity, sameValue } from '#shared/utils/editModel'

export type RelatedOrigin = 'human' | 'upstream'
export type RelatedState = 'added' | 'changed' | 'hidden' | null

export type RelatedValue = Record<string, unknown>

export interface RelatedRow<T = RelatedValue> {
  key: string
  origin: RelatedOrigin
  value: T
  index: number
  identity: string | null
  source: string
  state: RelatedState
}

const asRecord = (value: unknown): RelatedValue =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as RelatedValue)
    : {}

export const upstreamContextRows = (rows: CatalogEditContextRow[] | null) =>
  (rows ?? []).filter((row) => !row.human_lane && row.present)

export const humanRowKey = (index: number) => `h:${index}`
export const upstreamRowKey = (identity: string) => `u:${identity}`

export const mergeRelatedRows = <T = RelatedValue>(options: {
  fieldKey: string
  human: unknown[]
  base: unknown[]
  context: CatalogEditContextRow[] | null
  suppressed: string[]
}): RelatedRow<T>[] => {
  const baseById = new Map(
    options.base.map((row) => [rowIdentity(options.fieldKey, row), row])
  )
  const hidden = new Set(options.suppressed)
  const human: RelatedRow<T>[] = options.human.map((row, index) => {
    const prior = baseById.get(rowIdentity(options.fieldKey, row))
    return {
      key: humanRowKey(index),
      origin: 'human',
      value: row as T,
      index,
      identity: null,
      source: '',
      state:
        prior === undefined ? 'added' : sameValue(prior, row) ? null : 'changed'
    }
  })
  const upstream: RelatedRow<T>[] = upstreamContextRows(options.context)
    .filter((row) => row.identity)
    .map((row) => ({
      key: upstreamRowKey(row.identity!),
      origin: 'upstream',
      value: asRecord(row.value) as T,
      index: -1,
      identity: row.identity,
      source: sourceLabel(row.source),
      state: hidden.has(row.identity!) ? 'hidden' : null
    }))
  return [...human, ...upstream]
}

export const visibleRows = <T>(rows: RelatedRow<T>[]) =>
  rows.filter((row) => row.state !== 'hidden')

export const hiddenRows = <T>(rows: RelatedRow<T>[]) =>
  rows.filter((row) => row.state === 'hidden')

export const groupRows = <T>(
  rows: RelatedRow<T>[],
  keyOf: (row: RelatedRow<T>) => string,
  order: string[] = []
) => {
  const groups = new Map<string, RelatedRow<T>[]>()
  for (const row of rows) {
    const key = keyOf(row)
    groups.set(key, [...(groups.get(key) ?? []), row])
  }
  const rank = (key: string) => {
    const at = order.indexOf(key)
    return at < 0 ? order.length : at
  }
  return [...groups.entries()]
    .sort(([a], [b]) => rank(a) - rank(b))
    .map(([key, items]) => ({ key, items }))
}

export const matchesFilter = (query: string, ...texts: unknown[]) => {
  const q = query.trim().toLowerCase()
  if (!q) {
    return true
  }
  return texts.some((text) =>
    String(text ?? '')
      .toLowerCase()
      .includes(q)
  )
}

export const relationPhrase = (relation: string, reverse: boolean) => {
  const entry = WORK_RELATION_PHRASES[relation]
  if (!entry) {
    return relation
  }
  return reverse && !entry.symmetric ? entry.reverse : entry.forward
}

export const creditKeyClash = (
  candidate: RelatedValue,
  upstream: RelatedRow[]
) => {
  const key = (row: RelatedValue) =>
    `${String(row.role_id ?? '')}:${String(row.credit_name_id ?? '')}:${String(row.character_id ?? '') || '0'}`
  const wanted = key(candidate)
  return upstream.some(
    (row) => row.origin === 'upstream' && key(row.value) === wanted
  )
}

const SOURCE_PATTERNS: [RegExp, string][] = [
  [/vndb/i, 'VNDB'],
  [/bangumi|bgm/i, 'Bangumi'],
  [/erogamescape|egs/i, 'EGS'],
  [/dlsite/i, 'DLsite'],
  [/getchu/i, 'Getchu'],
  [/steam/i, 'Steam'],
  [/howlongtobeat|hltb/i, 'HLTB'],
  [/curated|user|manual/i, '人工']
]

export const sourceLabel = (
  source: { key?: string; display?: string } | null | undefined
) => {
  const text = `${source?.key ?? ''} ${source?.display ?? ''}`
  const hit = SOURCE_PATTERNS.find(([pattern]) => pattern.test(text))
  return hit ? hit[1] : source?.display || source?.key || ''
}

export interface RelatedIdRow {
  key: string
  id: string
  origin: RelatedOrigin
  index: number
  identity: string | null
  source: string
  state: RelatedState
}

const ID_KEYS = [
  'series_id',
  'engine_id',
  'tag_id',
  'label_id',
  'work_id',
  'id'
]

export const idOfContextValue = (value: unknown) => {
  if (value === null || value === undefined) {
    return ''
  }
  if (typeof value !== 'object') {
    return String(value)
  }
  const record = value as RelatedValue
  const key = ID_KEYS.find((one) => record[one] != null)
  return key ? String(record[key]) : ''
}

export const mergeIdRows = (options: {
  human: unknown[]
  base: unknown[]
  context: CatalogEditContextRow[] | null
  suppressed: string[]
}): RelatedIdRow[] => {
  const baseIds = new Set(options.base.map((id) => String(id)))
  const hidden = new Set(options.suppressed)
  const humanIds = new Set(options.human.map((id) => String(id)))
  const human: RelatedIdRow[] = options.human.map((raw, index) => ({
    key: humanRowKey(index),
    id: String(raw),
    origin: 'human',
    index,
    identity: null,
    source: '',
    state: baseIds.has(String(raw)) ? null : 'added'
  }))
  const upstream: RelatedIdRow[] = upstreamContextRows(options.context)
    .filter((row) => row.identity)
    .map((row) => ({
      key: upstreamRowKey(row.identity!),
      id: idOfContextValue(row.value),
      origin: 'upstream' as const,
      index: -1,
      identity: row.identity,
      source: sourceLabel(row.source),
      state: hidden.has(row.identity!) ? ('hidden' as const) : null
    }))
    .filter((row) => row.id && !humanIds.has(row.id))
  return [...human, ...upstream]
}
