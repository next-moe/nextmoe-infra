import type { EditUpstreamRow } from '#shared/types/edit'
import { sameValue } from '#shared/utils/editModel'

export type MergedRowLane = 'human' | 'upstream' | 'ghost'

export type MergedRowStatus =
  'kept' | 'added' | 'changed' | 'removed' | 'hidden' | 'hiding' | 'unhiding'

export interface MergedRow {
  key: string
  lane: MergedRowLane
  index: number
  value: unknown
  base?: unknown
  upstream?: EditUpstreamRow
  identity: string | null
  status: MergedRowStatus
}

export interface MergeInput {
  draft: unknown[]
  base: unknown[]
  upstream: EditUpstreamRow[]
  suppressed: string[]
  baseSuppressed: string[]
  identityOf: (row: unknown) => string
}

const isHiddenStatus = (status: MergedRowStatus) =>
  status === 'hidden' || status === 'hiding'

export const isMergedRowHidden = (row: MergedRow) => isHiddenStatus(row.status)

export const isMergedRowVisible = (row: MergedRow) =>
  row.status !== 'removed' && !isHiddenStatus(row.status)

export const mergeRows = (input: MergeInput): MergedRow[] => {
  const draftIds = input.draft.map(input.identityOf)
  const draftSet = new Set(draftIds)
  const baseIds = input.base.map(input.identityOf)
  const baseIndexById = new Map(baseIds.map((id, index) => [id, index]))
  const orphanBase = new Set(
    baseIds.flatMap((id, index) => (draftSet.has(id) ? [] : [index]))
  )
  const hidden = new Set(input.suppressed)
  const wasHidden = new Set(input.baseSuppressed)

  const human: MergedRow[] = input.draft.map((row, index) => {
    const identity = draftIds[index]!
    let baseIndex = baseIndexById.get(identity)
    if (baseIndex === undefined && orphanBase.has(index)) {
      baseIndex = index
      orphanBase.delete(index)
    }
    const base = baseIndex === undefined ? undefined : input.base[baseIndex]
    return {
      key: `h:${index}`,
      lane: 'human',
      index,
      value: row,
      base,
      identity,
      status:
        base === undefined ? 'added' : sameValue(base, row) ? 'kept' : 'changed'
    }
  })

  const removed: MergedRow[] = [...orphanBase].map((index) => ({
    key: `r:${baseIds[index]}:${index}`,
    lane: 'human',
    index,
    value: input.base[index],
    base: input.base[index],
    identity: baseIds[index]!,
    status: 'removed'
  }))

  const upstreamIdentities = new Set<string>()
  const upstream: MergedRow[] = []
  for (const row of input.upstream) {
    if (row.match && draftSet.has(row.match)) {
      continue
    }
    if (row.identity) {
      upstreamIdentities.add(row.identity)
    }
    const isHidden = Boolean(row.identity && hidden.has(row.identity))
    const washidden = Boolean(row.identity && wasHidden.has(row.identity))
    upstream.push({
      key: `u:${row.key}`,
      lane: 'upstream',
      index: -1,
      value: row.raw,
      upstream: row,
      identity: row.identity,
      status: isHidden
        ? washidden
          ? 'hidden'
          : 'hiding'
        : washidden
          ? 'unhiding'
          : 'kept'
    })
  }

  const ghostKeys = [
    ...new Set([...input.suppressed, ...input.baseSuppressed])
  ].filter((key) => !upstreamIdentities.has(key) && !draftSet.has(key))
  const ghosts: MergedRow[] = ghostKeys.map((key) => ({
    key: `g:${key}`,
    lane: 'ghost',
    index: -1,
    value: null,
    identity: key,
    status: hidden.has(key)
      ? wasHidden.has(key)
        ? 'hidden'
        : 'hiding'
      : 'unhiding'
  }))

  const humanAndRemoved = [...human]
  for (const row of removed) {
    const at = humanAndRemoved.findIndex(
      (entry) =>
        entry.lane === 'human' &&
        entry.index >= row.index &&
        entry.status !== 'removed'
    )
    humanAndRemoved.splice(at < 0 ? humanAndRemoved.length : at, 0, row)
  }
  const visibleUpstream = upstream.filter((row) => !isHiddenStatus(row.status))
  const hiddenUpstream = upstream.filter((row) => isHiddenStatus(row.status))
  return [...humanAndRemoved, ...visibleUpstream, ...hiddenUpstream, ...ghosts]
}

export const toggleSuppression = (suppressed: string[], identity: string) =>
  suppressed.includes(identity)
    ? suppressed.filter((key) => key !== identity)
    : [...suppressed, identity]

export const restoreRemovedRow = (
  draft: unknown[],
  base: unknown[],
  row: MergedRow
) => {
  if (row.status !== 'removed') {
    return draft
  }
  const insertAt = Math.min(row.index, draft.length)
  return [...draft.slice(0, insertAt), row.base, ...draft.slice(insertAt)]
}

export const summarizeMerged = (rows: MergedRow[]) => {
  const out = { visible: 0, hidden: 0, human: 0, upstream: 0, changed: 0 }
  for (const row of rows) {
    if (isMergedRowVisible(row)) {
      out.visible++
      if (row.lane === 'human') {
        out.human++
      } else if (row.lane === 'upstream') {
        out.upstream++
      }
    } else if (isMergedRowHidden(row)) {
      out.hidden++
    }
    if (row.status !== 'kept' && row.status !== 'hidden') {
      out.changed++
    }
  }
  return out
}
