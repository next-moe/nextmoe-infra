import { rowIdentity, sameValue } from './editModel'

const canonicalize = (value: unknown): unknown => {
  if (Array.isArray(value)) {
    return value.map(canonicalize)
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value as Record<string, unknown>)
        .sort()
        .map((key) => [
          key,
          canonicalize((value as Record<string, unknown>)[key])
        ])
    )
  }
  return value === undefined ? null : value
}

export const canonicalJson = (value: unknown) =>
  JSON.stringify(canonicalize(value))

export const valuesEqual = (a: unknown, b: unknown) =>
  canonicalJson(a) === canonicalJson(b)

export type DiffOp = 'same' | 'add' | 'del'

export interface DiffSegment {
  op: DiffOp
  text: string
}

const MAX_CELLS = 250_000

const TOKEN = /[A-Za-z0-9_]+|\s+|[\s\S]/gu

export const tokenize = (text: string) => text.match(TOKEN) ?? []

const splitLines = (text: string) => text.match(/[^\n]*\n|[^\n]+/g) ?? []

const push = (out: DiffSegment[], op: DiffOp, text: string) => {
  if (!text) {
    return
  }
  const last = out.at(-1)
  if (last && last.op === op) {
    last.text += text
  } else {
    out.push({ op, text })
  }
}

const lcsDiff = (a: string[], b: string[], out: DiffSegment[]) => {
  const n = a.length
  const m = b.length
  if (n * m > MAX_CELLS) {
    push(out, 'del', a.join(''))
    push(out, 'add', b.join(''))
    return
  }
  const width = m + 1
  const table = new Uint32Array((n + 1) * width)
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i * width + j] =
        a[i] === b[j]
          ? table[(i + 1) * width + j + 1]! + 1
          : Math.max(table[(i + 1) * width + j]!, table[i * width + j + 1]!)
    }
  }
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      push(out, 'same', a[i]!)
      i++
      j++
    } else if (table[(i + 1) * width + j]! >= table[i * width + j + 1]!) {
      push(out, 'del', a[i]!)
      i++
    } else {
      push(out, 'add', b[j]!)
      j++
    }
  }
  while (i < n) {
    push(out, 'del', a[i++]!)
  }
  while (j < m) {
    push(out, 'add', b[j++]!)
  }
}

const diffTokens = (a: string[], b: string[]) => {
  const out: DiffSegment[] = []
  let start = 0
  while (start < a.length && start < b.length && a[start] === b[start]) {
    start++
  }
  let endA = a.length
  let endB = b.length
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--
    endB--
  }
  push(out, 'same', a.slice(0, start).join(''))
  lcsDiff(a.slice(start, endA), b.slice(start, endB), out)
  push(out, 'same', a.slice(endA).join(''))
  return out
}

export const diffText = (before: string, after: string) =>
  diffTokens(tokenize(before), tokenize(after))

export const diffLines = (before: string, after: string) =>
  diffTokens(splitLines(before), splitLines(after))

const PROSE_TOKEN_LIMIT = 6000

export const diffProse = (before: string, after: string) =>
  before.length + after.length > PROSE_TOKEN_LIMIT
    ? diffLines(before, after)
    : diffText(before, after)

export const hasChanges = (segments: DiffSegment[]) =>
  segments.some((segment) => segment.op !== 'same')

export type ListEntryStatus = 'kept' | 'moved' | 'changed' | 'added' | 'removed'

export interface ListEntry {
  status: ListEntryStatus
  before?: unknown
  after?: unknown
  fromIndex?: number
  toIndex?: number
}

export interface ListDiffSummary {
  added: number
  removed: number
  changed: number
  moved: number
}

const NATURAL_KEYS: Record<string, string[]> = {
  titles: ['lang', 'kind'],
  intros: ['lang'],
  labels: ['label_id', 'kind'],
  credits: ['role_id', 'credit_name_id', 'character_id'],
  credits_upstream: ['role_id', 'credit_name_id', 'character_id'],
  roster: ['character_id'],
  covers: ['image_hash'],
  screenshots: ['image_hash'],
  covers_upstream: ['image_hash'],
  screenshots_upstream: ['image_hash'],
  traits: ['trait_id'],
  relations: ['label_id', 'relation'],
  aliases: ['lang', 'name', 'alias']
}

const KEY_MEMBER_TYPES = new Set(['ref', 'imagehash'])

export interface ElementMemberShape {
  key: string
  type: string
}

export const fieldSuffix = (fieldKey: string) =>
  fieldKey
    .replace(/\.suppressed$/, '')
    .split('.')
    .pop() ?? fieldKey

export const naturalKeyMembers = (
  fieldKey: string,
  members: ElementMemberShape[]
) => {
  const known = NATURAL_KEYS[fieldSuffix(fieldKey)]
  const present = new Set(members.map((member) => member.key))
  if (known) {
    const keys = known.filter((key) => present.has(key))
    if (keys.length) {
      return keys
    }
  }
  return members
    .filter(
      (member) => KEY_MEMBER_TYPES.has(member.type) || member.key === 'lang'
    )
    .map((member) => member.key)
}

export const elementKey = (item: unknown, keyMembers: string[]) => {
  if (
    item &&
    typeof item === 'object' &&
    !Array.isArray(item) &&
    keyMembers.length
  ) {
    const record = item as Record<string, unknown>
    return canonicalJson(keyMembers.map((key) => record[key] ?? null))
  }
  return canonicalJson(item)
}

const longestIncreasing = (values: number[]) => {
  const tails: number[] = []
  const tailIndex: number[] = []
  const previous = new Array<number>(values.length).fill(-1)
  values.forEach((value, i) => {
    let lo = 0
    let hi = tails.length
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (tails[mid]! < value) {
        lo = mid + 1
      } else {
        hi = mid
      }
    }
    tails[lo] = value
    tailIndex[lo] = i
    previous[i] = lo > 0 ? tailIndex[lo - 1]! : -1
  })
  const keep = new Set<number>()
  let cursor = tails.length ? tailIndex[tails.length - 1]! : -1
  while (cursor >= 0) {
    keep.add(cursor)
    cursor = previous[cursor]!
  }
  return keep
}

export const alignList = (
  before: readonly unknown[],
  after: readonly unknown[],
  keyMembers: string[] = []
): ListEntry[] => {
  const index = (keyOf: (item: unknown) => string, free: Set<number>) => {
    const queues = new Map<string, number[]>()
    for (const fromIndex of free) {
      const key = keyOf(before[fromIndex])
      queues.set(key, [...(queues.get(key) ?? []), fromIndex])
    }
    return queues
  }
  const take = (
    queues: Map<string, number[]>,
    key: string,
    free: Set<number>
  ) => {
    const fromIndex = queues.get(key)?.shift()
    if (fromIndex !== undefined) {
      free.delete(fromIndex)
    }
    return fromIndex
  }

  const free = new Set(before.map((_, i) => i))
  const entries: (ListEntry | null)[] = new Array(after.length).fill(null)
  const exact = index(canonicalJson, free)
  const kept: { toIndex: number; fromIndex: number }[] = []
  after.forEach((item, toIndex) => {
    const fromIndex = take(exact, canonicalJson(item), free)
    if (fromIndex !== undefined) {
      kept.push({ toIndex, fromIndex })
      entries[toIndex] = {
        status: 'kept',
        before: before[fromIndex],
        after: item,
        fromIndex,
        toIndex
      }
    }
  })

  const natural = index((item) => elementKey(item, keyMembers), free)
  after.forEach((item, toIndex) => {
    if (entries[toIndex]) {
      return
    }
    const fromIndex = take(natural, elementKey(item, keyMembers), free)
    entries[toIndex] =
      fromIndex === undefined
        ? { status: 'added', after: item, toIndex }
        : {
            status: valuesEqual(before[fromIndex], item) ? 'kept' : 'changed',
            before: before[fromIndex],
            after: item,
            fromIndex,
            toIndex
          }
  })

  const inOrder = longestIncreasing(kept.map((m) => m.fromIndex))
  kept.forEach((m, i) => {
    if (!inOrder.has(i)) {
      entries[m.toIndex]!.status = 'moved'
    }
  })

  const removed: ListEntry[] = [...free]
    .sort((a, b) => a - b)
    .map((fromIndex) => ({
      status: 'removed',
      before: before[fromIndex],
      fromIndex
    }))
  return [...(entries as ListEntry[]), ...removed]
}

export const summarizeList = (entries: ListEntry[]): ListDiffSummary =>
  entries.reduce<ListDiffSummary>(
    (sum, entry) => {
      if (entry.status === 'added') sum.added++
      if (entry.status === 'removed') sum.removed++
      if (entry.status === 'changed') sum.changed++
      if (entry.status === 'moved') sum.moved++
      return sum
    },
    { added: 0, removed: 0, changed: 0, moved: 0 }
  )

export const asList = (value: unknown): unknown[] =>
  Array.isArray(value) ? value : value == null ? [] : [value]

export interface EditListDiff<T = unknown> {
  added: T[]
  removed: T[]
  changed: { before: T; after: T }[]
  unchanged: number
  reordered: boolean
}

export const diffFieldList = <T>(
  fieldKey: string,
  before: T[] | null | undefined,
  after: T[] | null | undefined
): EditListDiff<T> => {
  const prev = before ?? []
  const next = after ?? []
  const prevById = new Map(prev.map((row) => [rowIdentity(fieldKey, row), row]))
  const nextById = new Map(next.map((row) => [rowIdentity(fieldKey, row), row]))

  const diff: EditListDiff<T> = {
    added: [],
    removed: [],
    changed: [],
    unchanged: 0,
    reordered: false
  }
  for (const [id, row] of nextById) {
    const old = prevById.get(id)
    if (old === undefined) {
      diff.added.push(row)
    } else if (sameValue(old, row)) {
      diff.unchanged++
    } else {
      diff.changed.push({ before: old, after: row })
    }
  }
  for (const [id, row] of prevById) {
    if (!nextById.has(id)) {
      diff.removed.push(row)
    }
  }
  const keptPrev = prev
    .map((row) => rowIdentity(fieldKey, row))
    .filter((id) => nextById.has(id))
  const keptNext = next
    .map((row) => rowIdentity(fieldKey, row))
    .filter((id) => prevById.has(id))
  diff.reordered = keptPrev.join('\u0000') !== keptNext.join('\u0000')
  return diff
}

export interface EditTextDiffLine {
  type: 'same' | 'add' | 'del'
  text: string
}

export const diffLineRows = (
  before: string,
  after: string
): EditTextDiffLine[] =>
  diffLines(before, after).flatMap((segment) =>
    segment.text
      .replace(/\n$/, '')
      .split('\n')
      .map((text) => ({ type: segment.op, text }))
  )
