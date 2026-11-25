import type {
  CanonEditChange,
  CanonProblemCandidate,
  CanonSchemaEntity
} from '#shared/types/canon'
import type { CanonCreatePart, CanonCreateTemplate } from '~/constants/canonCreate'
import { typedValue } from './record'

export type CanonDrafts = Record<string, Record<string, string | boolean>>

export const draftsFor = (template: CanonCreateTemplate): CanonDrafts =>
  Object.fromEntries(
    template.parts.map((p) => [
      p.ref,
      Object.fromEntries(p.fields.map((f) => [f, p.defaults?.[f] ?? '']))
    ])
  )

export const missingFields = (template: CanonCreateTemplate, drafts: CanonDrafts) =>
  template.parts.flatMap((p) =>
    p.required
      .filter((f) => String(drafts[p.ref]?.[f] ?? '').trim() === '')
      .map((f) => ({ entity: p.entity, field: f }))
  )

const presetValue = (value: string, context: Record<string, string>) =>
  value.startsWith('@') ? (context[value.slice(1)] ?? '') : value

/** A row whose every field was left empty is not written, unless it is structural. */
const isEmpty = (part: CanonCreatePart, values: Record<string, unknown>) =>
  part.fields.length > 0 && part.required.length === 0 && !Object.keys(values).length

export const changesFor = (
  template: CanonCreateTemplate,
  drafts: CanonDrafts,
  schemas: CanonSchemaEntity[],
  context: Record<string, string>
): CanonEditChange[] =>
  template.parts.flatMap((part) => {
    const schema = schemas.find((s) => s.name === part.entity)
    const values: Record<string, unknown> = {}
    for (const field of part.fields) {
      const property = schema?.properties.find((p) => p.key === field)
      if (!property) {
        continue
      }
      const value = typedValue(property, drafts[part.ref]?.[field] ?? '')
      if (value !== undefined) {
        values[field] = value
      }
    }
    if (isEmpty(part, values)) {
      return []
    }
    for (const [key, value] of Object.entries(part.preset)) {
      values[key] = presetValue(value, context)
    }
    return [{ ref: part.ref, entity: part.entity, values }]
  })

/** Which change of a template a candidate's identity is judged on. */
export const judgedRef = (template: CanonCreateTemplate) => {
  const entities = ['work', 'person_alias', 'org']
  return template.parts.find((p) => entities.includes(p.entity))?.ref ?? template.parts[0]!.ref
}

export const resolvedChanges = (
  changes: CanonEditChange[],
  ref: string,
  candidates: CanonProblemCandidate[],
  adopt: string
): CanonEditChange[] =>
  changes.map((c) => {
    if (c.ref !== ref) {
      return c
    }
    const others = candidates.filter((x) => x.token !== `legacy:${adopt}`)
    return {
      ...c,
      ...(adopt ? { adopt_id: adopt } : {}),
      distinct: others.map((x) => x.token)
    }
  })
