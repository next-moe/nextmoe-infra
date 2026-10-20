import type { CatalogEditContextField } from '#shared/types/catalog'

export type MediaKind = 'cover' | 'screenshot'

export interface MediaFieldKeys {
  human: string
  upstream: string
  suppressed: string
  order: string
  preset: 'cover' | 'screenshot'
}

export const MEDIA_FIELDS: Record<MediaKind, MediaFieldKeys> = {
  cover: {
    human: 'catalog.work.covers',
    upstream: 'catalog.work.covers_upstream',
    suppressed: 'catalog.work.covers.suppressed',
    order: 'catalog.work.cover_order',
    preset: 'cover'
  },
  screenshot: {
    human: 'catalog.work.screenshots',
    upstream: 'catalog.work.screenshots_upstream',
    suppressed: 'catalog.work.screenshots.suppressed',
    order: 'catalog.work.screenshot_order',
    preset: 'screenshot'
  }
}

export type MediaRow = Record<string, unknown>

export interface MediaContextRow {
  hash: string
  human: boolean
  identity: string | null
  source: string | null
}

export interface MediaTile {
  hash: string
  lane: 'human' | 'upstream'
  index: number
  row: MediaRow
  baseRow: MediaRow | null
  identity: string | null
  source: string | null
  hidden: boolean
  baseHidden: boolean
  isNew: boolean
  changed: boolean
}

export interface MediaState {
  human: MediaRow[]
  baseHuman: MediaRow[]
  upstream: MediaRow[]
  baseUpstream: MediaRow[]
  suppressed: string[]
  baseSuppressed: string[]
  order: string[]
  baseOrder: string[]
  context: MediaContextRow[]
}

const hashOf = (row: MediaRow) => String(row.image_hash ?? '')

const canonical = (value: unknown): string =>
  JSON.stringify(value, (_, v) =>
    v && typeof v === 'object' && !Array.isArray(v)
      ? Object.fromEntries(
          Object.entries(v as Record<string, unknown>)
            .filter(([, inner]) => inner !== undefined && inner !== null)
            .sort(([a], [b]) => a.localeCompare(b))
        )
      : v
  )

export const sameMediaRow = (a: MediaRow | null, b: MediaRow | null) =>
  canonical(a ?? null) === canonical(b ?? null)

export const asMediaRows = (value: unknown): MediaRow[] =>
  Array.isArray(value)
    ? value.filter(
        (row): row is MediaRow =>
          Boolean(row) && typeof row === 'object' && !Array.isArray(row)
      )
    : []

export const asHashList = (value: unknown): string[] =>
  Array.isArray(value) ? value.map(String).filter(Boolean) : []

export const contextMediaRows = (
  field: CatalogEditContextField | undefined
): MediaContextRow[] =>
  (field?.rows ?? [])
    .filter((row) => row.present)
    .map((row) => {
      const value = (row.value ?? {}) as MediaRow
      return {
        hash: hashOf(value),
        human: row.human_lane,
        identity: row.identity,
        source: row.source?.display ?? null
      }
    })
    .filter((row) => row.hash)

const unique = (hashes: string[]) => [...new Set(hashes)]

export const naturalSequence = (state: MediaState) =>
  unique([
    ...state.context.map((row) => row.hash),
    ...state.human.map(hashOf),
    ...state.upstream.map(hashOf)
  ])

export const applyOrder = (sequence: string[], order: string[]) => {
  if (!order.length) {
    return sequence
  }
  const present = new Set(sequence)
  const listed = order.filter((hash) => present.has(hash))
  const listedSet = new Set(listed)
  return [...listed, ...sequence.filter((hash) => !listedSet.has(hash))]
}

export const sameList = (a: string[], b: string[]) =>
  a.length === b.length && a.every((value, i) => value === b[i])

export const displaySequence = (state: MediaState) => {
  const natural = naturalSequence(state)
  return sameList(state.order, state.baseOrder)
    ? natural
    : applyOrder(natural, state.order)
}

export const orderResetPending = (state: MediaState) =>
  state.order.length === 0 && state.baseOrder.length > 0

export const buildMediaTiles = (state: MediaState): MediaTile[] => {
  const contextByHash = new Map(state.context.map((row) => [row.hash, row]))
  const baseHumanByHash = new Map(state.baseHuman.map((r) => [hashOf(r), r]))
  const baseUpstreamByHash = new Map(
    state.baseUpstream.map((r) => [hashOf(r), r])
  )
  const suppressed = new Set(state.suppressed)
  const baseSuppressed = new Set(state.baseSuppressed)
  const tiles = new Map<string, MediaTile>()

  state.human.forEach((row, index) => {
    const hash = hashOf(row)
    const baseRow = baseHumanByHash.get(hash) ?? null
    tiles.set(hash, {
      hash,
      lane: 'human',
      index,
      row,
      baseRow,
      identity: null,
      source: null,
      hidden: false,
      baseHidden: false,
      isNew: baseRow === null,
      changed: baseRow === null || !sameMediaRow(baseRow, row)
    })
  })
  state.upstream.forEach((row, index) => {
    const hash = hashOf(row)
    if (tiles.has(hash)) {
      return
    }
    const context = contextByHash.get(hash)
    const identity = context && !context.human ? context.identity : null
    const baseRow = baseUpstreamByHash.get(hash) ?? null
    const hidden = Boolean(identity && suppressed.has(identity))
    const baseHidden = Boolean(identity && baseSuppressed.has(identity))
    tiles.set(hash, {
      hash,
      lane: 'upstream',
      index,
      row,
      baseRow,
      identity,
      source: context?.source ?? null,
      hidden,
      baseHidden,
      isNew: false,
      changed: hidden !== baseHidden || !sameMediaRow(baseRow, row)
    })
  })

  return displaySequence(state)
    .map((hash) => tiles.get(hash))
    .filter((tile): tile is MediaTile => Boolean(tile))
}

export const removedHumanRows = (state: MediaState) => {
  const kept = new Set(state.human.map(hashOf))
  return state.baseHuman.filter((row) => !kept.has(hashOf(row)))
}

export const moveHash = (sequence: string[], hash: string, to: number) => {
  const from = sequence.indexOf(hash)
  if (from < 0) {
    return sequence
  }
  const next = [...sequence]
  next.splice(from, 1)
  const target = Math.max(0, Math.min(to > from ? to - 1 : to, next.length))
  next.splice(target, 0, hash)
  return next
}

export const orderValueFor = (sequence: string[], state: MediaState) =>
  sameList(sequence, naturalSequence(state)) ? [...state.baseOrder] : sequence

const withoutKey = (row: MediaRow, key: string) => {
  const { [key]: _, ...rest } = row
  return rest
}

export const setPinned = (
  state: Pick<MediaState, 'human' | 'upstream'>,
  hash: string | null
) => {
  const human = state.human.map((row) => {
    const pin = hashOf(row) === hash
    if (pin === Boolean(row.portrait_pinned)) {
      return row
    }
    return pin
      ? { ...row, portrait_pinned: true }
      : withoutKey(row, 'portrait_pinned')
  })
  const upstream = state.upstream.map((row) => {
    const pin = hashOf(row) === hash
    return pin === Boolean(row.portrait_pinned)
      ? row
      : { ...row, portrait_pinned: pin }
  })
  return { human, upstream }
}

export const setRowMember = (
  rows: MediaRow[],
  index: number,
  key: string,
  value: unknown,
  omitEmpty: boolean
) =>
  rows.map((row, i) => {
    if (i !== index) {
      return row
    }
    const empty =
      value === null ||
      value === undefined ||
      value === '' ||
      value === 0 ||
      value === false
    return omitEmpty && empty ? withoutKey(row, key) : { ...row, [key]: value }
  })

export const COVER_KINDS: { value: string; label: string }[] = [
  { value: 'main', label: '主视觉' },
  { value: 'pkgfront', label: '包装正面' },
  { value: 'pkgback', label: '包装背面' },
  { value: 'pkgside', label: '包装侧面' },
  { value: 'pkgcontent', label: '包装内容' },
  { value: 'pkgmed', label: '光盘介质' },
  { value: 'dig', label: '数字版封面' },
  { value: '', label: '未分类' }
]

export const coverKindLabel = (kind: unknown) => {
  const value = String(kind ?? '')
  return COVER_KINDS.find((entry) => entry.value === value)?.label ?? value
}

const GRADE_LABELS: Record<string, string[]> = {
  sexual: ['安全', '擦边', '露骨'],
  violence: ['无', '暴力', '血腥']
}

const MEMBER_CHANGE_LABELS: Record<string, string> = {
  portrait_pinned: '主封面',
  kind: '类型',
  caption: '说明',
  sexual: '性暗示',
  violence: '暴力'
}

const memberText = (key: string, value: unknown) => {
  if (key === 'portrait_pinned') {
    return value ? '是' : '否'
  }
  if (key === 'kind') {
    return coverKindLabel(value)
  }
  if (key in GRADE_LABELS) {
    return GRADE_LABELS[key]![Number(value ?? 0)] ?? String(value)
  }
  return String(value ?? '') || '（空）'
}

export const mediaRowChanges = (before: MediaRow | null, after: MediaRow) =>
  Object.keys(MEMBER_CHANGE_LABELS)
    .filter((key) => key in after || (before && key in before))
    .filter(
      (key) => memberText(key, before?.[key]) !== memberText(key, after[key])
    )
    .map((key) => ({
      label: MEMBER_CHANGE_LABELS[key]!,
      before: memberText(key, before?.[key]),
      after: memberText(key, after[key])
    }))
