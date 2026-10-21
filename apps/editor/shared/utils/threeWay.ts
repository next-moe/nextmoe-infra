import type { CatalogSchemaField } from '../types/catalog'
import {
  isSuppressedKey,
  normalizeFieldValue,
  rowIdentity,
  sameValue,
  sortedIdentities,
  stableStringify
} from './editModel'

export interface ThreeWayOverlap {
  identity: string
  members: string[]
  base?: unknown
  current?: unknown
  proposed?: unknown
}

export interface ThreeWayMerge {
  clean: boolean
  value: unknown
  overlaps: ThreeWayOverlap[]
}

type Row = Record<string, unknown>

const isRow = (value: unknown): value is Row =>
  value !== null && typeof value === 'object' && !Array.isArray(value)

const asList = (value: unknown): unknown[] =>
  Array.isArray(value) ? value : []

const clean = (value: unknown): ThreeWayMerge => ({
  clean: true,
  value,
  overlaps: []
})

const mergeMembers = (base: Row | undefined, current: Row, proposed: Row) => {
  const keys = new Set([...Object.keys(current), ...Object.keys(proposed)])
  const value: Row = {}
  const conflicts: string[] = []
  for (const key of keys) {
    const b = base?.[key]
    const c = current[key]
    const p = proposed[key]
    let chosen = c
    if (sameValue(c, b)) {
      chosen = p
    } else if (!sameValue(p, b) && !sameValue(c, p)) {
      conflicts.push(key)
    }
    if (chosen !== undefined) {
      value[key] = chosen
    }
  }
  return { value, conflicts }
}

const mergeSets = (
  field: CatalogSchemaField,
  base: unknown[],
  current: unknown[],
  proposed: unknown[]
): ThreeWayMerge => {
  const keyOf = (item: unknown) =>
    typeof item === 'string' ? item : stableStringify(item)
  const baseKeys = new Set(base.map(keyOf))
  const proposedKeys = new Set(proposed.map(keyOf))
  const removed = new Set([...baseKeys].filter((k) => !proposedKeys.has(k)))
  const kept = current.filter((item) => !removed.has(keyOf(item)))
  const keptKeys = new Set(kept.map(keyOf))
  const added = proposed.filter(
    (item) => !baseKeys.has(keyOf(item)) && !keptKeys.has(keyOf(item))
  )
  const merged = [...kept, ...added]
  return clean(
    isSuppressedKey(field.key) ? sortedIdentities(merged.map(String)) : merged
  )
}

const mergeRows = (
  field: CatalogSchemaField,
  base: unknown[],
  current: unknown[],
  proposed: unknown[]
): ThreeWayMerge => {
  const identity = (row: unknown) => rowIdentity(field.key, row)
  const byIdentity = (rows: unknown[]) =>
    new Map(rows.map((row) => [identity(row), row]))
  const baseRows = byIdentity(base)
  const currentRows = byIdentity(current)
  const proposedRows = byIdentity(proposed)
  const overlaps: ThreeWayOverlap[] = []
  const value: unknown[] = []

  for (const row of current) {
    const id = identity(row)
    const b = baseRows.get(id)
    const p = proposedRows.get(id)
    const inBase = baseRows.has(id)
    const inProposed = proposedRows.has(id)
    if (!inProposed) {
      if (inBase && !sameValue(row, b)) {
        overlaps.push({ identity: id, members: [], base: b, current: row })
        value.push(row)
      } else if (!inBase) {
        value.push(row)
      }
      continue
    }
    if (sameValue(row, p)) {
      value.push(row)
      continue
    }
    if (!isRow(row) || !isRow(p)) {
      if (inBase && sameValue(row, b)) {
        value.push(p)
      } else if (inBase && sameValue(p, b)) {
        value.push(row)
      } else {
        overlaps.push({
          identity: id,
          members: [],
          base: b,
          current: row,
          proposed: p
        })
        value.push(row)
      }
      continue
    }
    const merged = mergeMembers(isRow(b) ? b : undefined, row, p)
    if (merged.conflicts.length) {
      overlaps.push({
        identity: id,
        members: merged.conflicts,
        base: b,
        current: row,
        proposed: p
      })
    }
    value.push(merged.value)
  }

  for (const [id, b] of baseRows) {
    if (currentRows.has(id) || !proposedRows.has(id)) {
      continue
    }
    const p = proposedRows.get(id)
    if (!sameValue(p, b)) {
      overlaps.push({ identity: id, members: [], base: b, proposed: p })
    }
  }

  for (const row of proposed) {
    const id = identity(row)
    if (!baseRows.has(id) && !currentRows.has(id)) {
      value.push(row)
    }
  }

  return { clean: overlaps.length === 0, value, overlaps }
}

export const threeWayMerge = (
  field: CatalogSchemaField,
  base: unknown,
  current: unknown,
  proposed: unknown
): ThreeWayMerge => {
  const b = normalizeFieldValue(field, base)
  const c = normalizeFieldValue(field, current)
  const p = normalizeFieldValue(field, proposed)
  if (sameValue(c, b)) {
    return clean(p)
  }
  if (sameValue(p, b) || sameValue(c, p)) {
    return clean(c)
  }
  if (field.field_type !== 'list') {
    return {
      clean: false,
      value: c,
      overlaps: [
        { identity: '', members: [], base: b, current: c, proposed: p }
      ]
    }
  }
  const lists = [asList(b), asList(c), asList(p)] as const
  const merged =
    field.element?.type === 'object'
      ? mergeRows(field, ...lists)
      : mergeSets(field, ...lists)
  return { ...merged, value: normalizeFieldValue(field, merged.value) }
}
