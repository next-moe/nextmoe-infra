import { rowIdentity, sameValue } from './editModel'

export interface EditListDiff<T = unknown> {
  added: T[]
  removed: T[]
  changed: { before: T; after: T }[]
  unchanged: number
  reordered: boolean
}

export const diffList = <T>(
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

const MAX_LCS_CELLS = 400_000

export const diffLines = (
  before: string,
  after: string
): EditTextDiffLine[] => {
  const a = before === '' ? [] : before.split('\n')
  const b = after === '' ? [] : after.split('\n')
  if (a.length * b.length > MAX_LCS_CELLS) {
    return [
      ...a.map((text) => ({ type: 'del' as const, text })),
      ...b.map((text) => ({ type: 'add' as const, text }))
    ]
  }
  const table: number[][] = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0)
  )
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      table[i]![j] =
        a[i] === b[j]
          ? table[i + 1]![j + 1]! + 1
          : Math.max(table[i + 1]![j]!, table[i]![j + 1]!)
    }
  }
  const out: EditTextDiffLine[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ type: 'same', text: a[i]! })
      i++
      j++
    } else if (table[i + 1]![j]! >= table[i]![j + 1]!) {
      out.push({ type: 'del', text: a[i]! })
      i++
    } else {
      out.push({ type: 'add', text: b[j]! })
      j++
    }
  }
  while (i < a.length) {
    out.push({ type: 'del', text: a[i++]! })
  }
  while (j < b.length) {
    out.push({ type: 'add', text: b[j++]! })
  }
  return out
}
