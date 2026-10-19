export interface RevisionLike {
  id: string
  seq: number
  changed_fields: string[]
}

export interface FieldDiffLike {
  key: string
  from: unknown
  to: unknown
}

const bareKey = (key: string) => key.replace(/\.suppressed$/, '')

export const conflictingFields = (
  baseSeq: number,
  revisions: readonly RevisionLike[],
  patchKeys: readonly string[]
) => {
  const touched = new Map<string, number>()
  for (const revision of revisions) {
    if (revision.seq <= baseSeq) {
      continue
    }
    for (const key of revision.changed_fields) {
      touched.set(key, Math.max(touched.get(key) ?? 0, revision.seq))
    }
  }
  const out: Record<string, number> = {}
  for (const key of patchKeys) {
    const seq = touched.get(key) ?? touched.get(bareKey(key))
    if (seq !== undefined) {
      out[key] = seq
    }
  }
  return out
}

export const latestRevision = <T extends RevisionLike>(
  revisions: readonly T[]
) =>
  revisions.reduce<T | null>(
    (latest, revision) =>
      !latest || revision.seq > latest.seq ? revision : latest,
    null
  )

export interface RevertChange {
  key: string
  current: unknown
  restored: unknown
}

export const revertChanges = (diff: readonly FieldDiffLike[]): RevertChange[] =>
  diff.map((entry) => ({
    key: entry.key,
    current: entry.to,
    restored: entry.from
  }))

export const orderPair = <T extends RevisionLike>(a: T, b: T) =>
  a.seq <= b.seq ? { older: a, newer: b } : { older: b, newer: a }
