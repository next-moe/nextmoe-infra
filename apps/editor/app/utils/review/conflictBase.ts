import {
  latestRevision,
  type FieldDiffLike,
  type RevisionLike
} from './history'

export interface ConflictBaseQuery {
  revisionId: string
  diffBase: string
}

export const conflictBaseQuery = (
  baseSeq: number,
  revisions: readonly RevisionLike[]
): ConflictBaseQuery | null => {
  const latest = latestRevision(revisions)
  if (!latest || latest.seq <= baseSeq) {
    return null
  }
  if (baseSeq === 0) {
    return { revisionId: latest.id, diffBase: 'baseline' }
  }
  const base = revisions.find((revision) => revision.seq === baseSeq)
  return base ? { revisionId: latest.id, diffBase: base.id } : null
}

export const basesFromDiff = (
  diff: readonly FieldDiffLike[],
  keys: readonly string[],
  current: Record<string, unknown>
) =>
  Object.fromEntries(
    keys.map((key) => {
      const entry = diff.find((item) => item.key === key)
      return [key, entry ? entry.from : current[key]]
    })
  )
