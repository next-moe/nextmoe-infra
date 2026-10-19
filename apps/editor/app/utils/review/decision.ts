import { valuesEqual } from './equal'

export type FieldVerdict = 'accept' | 'reject' | 'amend'

export interface FieldDecision {
  verdict: FieldVerdict
  value?: unknown
}

export type FieldDecisions = Record<string, FieldDecision>

export interface Amendment {
  set: Record<string, unknown>
  unset: string[]
}

export const initialDecisions = (
  effective: Record<string, unknown>
): FieldDecisions =>
  Object.fromEntries(
    Object.keys(effective).map((key) => [key, { verdict: 'accept' }])
  )

export const buildAmendment = (
  effective: Record<string, unknown>,
  decisions: FieldDecisions
): Amendment | null => {
  const set: Record<string, unknown> = {}
  const unset: string[] = []
  for (const key of Object.keys(effective)) {
    const decision = decisions[key]
    if (!decision) {
      continue
    }
    if (decision.verdict === 'reject') {
      unset.push(key)
    } else if (
      decision.verdict === 'amend' &&
      !valuesEqual(decision.value, effective[key])
    ) {
      set[key] = decision.value
    }
  }
  return Object.keys(set).length || unset.length ? { set, unset } : null
}

export const keysToMerge = (
  effective: Record<string, unknown>,
  decisions: FieldDecisions
) =>
  Object.keys(effective).filter((key) => decisions[key]?.verdict !== 'reject')

export const decisionCounts = (decisions: FieldDecisions) => {
  const counts = { accept: 0, reject: 0, amend: 0 }
  for (const decision of Object.values(decisions)) {
    counts[decision.verdict]++
  }
  return counts
}

export const neighbourIds = (ids: readonly string[], current: string) => {
  const index = ids.indexOf(current)
  if (index < 0) {
    return { previous: null, next: ids[0] ?? null }
  }
  return {
    previous: index > 0 ? ids[index - 1]! : null,
    next: index < ids.length - 1 ? ids[index + 1]! : null
  }
}

export const afterDecision = (ids: readonly string[], decided: string) => {
  const { previous, next } = neighbourIds(ids, decided)
  return {
    remaining: ids.filter((id) => id !== decided),
    target: next ?? previous
  }
}
