import type { CanonOutboxRow } from '#shared/types/canon'
import { CANON_CITABLE_ENTITIES } from '~/constants/canon'

export interface SuggestionValue {
  key: string
  value: string
  tier?: string
}

export interface SuggestionLine {
  ref: string
  entity: string
  values: SuggestionValue[]
}

export const showValue = (value: unknown) => {
  if (typeof value === 'string') {
    return value.startsWith('ref:') ? `→ ${value.slice(4)}` : value
  }
  return JSON.stringify(value)
}

export const suggestionLines = (row: CanonOutboxRow): SuggestionLine[] => {
  if (row.candidate) {
    const c = row.candidate
    return [
      {
        ref: `${c.target.entity} ${c.target.id}`,
        entity: c.target.entity,
        values: [{ key: c.property, value: showValue(c.value), tier: c.tier }]
      }
    ]
  }
  return (row.changeset?.entities ?? []).map((e) => ({
    ref: e.ref,
    entity: e.entity,
    values: Object.entries(e.properties).map(([key, c]) => ({
      key,
      value: showValue(c.value),
      tier: c.tier
    }))
  }))
}

export const citableRefs = (row: CanonOutboxRow) =>
  (row.changeset?.entities ?? [])
    .filter((e) => CANON_CITABLE_ENTITIES.includes(e.entity))
    .map((e) => ({ ref: e.ref, entity: e.entity }))

export const useIDsFrom = (inputs: Record<string, string>) => {
  const out: Record<string, string> = {}
  for (const [ref, raw] of Object.entries(inputs)) {
    const id = raw.trim()
    if (/^[1-9][0-9]*$/.test(id)) {
      out[ref] = id
    }
  }
  return out
}
