import { canonicalJson, valuesEqual } from './equal'

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

export const diffList = (
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
