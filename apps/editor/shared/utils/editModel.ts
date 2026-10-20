import type { CatalogSchemaField } from '../types/catalog'
import type {
  CatalogSchemaMember,
  EditFieldValues,
  EditVocabulary
} from '../types/edit'
import { workTitleIdentity } from './textnorm'

export const SUPPRESSED_SUFFIX = '.suppressed'

export const isSuppressedKey = (key: string) => key.endsWith(SUPPRESSED_SUFFIX)

export const suppressedKeyOf = (key: string) => `${key}${SUPPRESSED_SUFFIX}`

export const bareFieldName = (key: string) => key.split('.').slice(2).join('.')

const canonical = (value: unknown): unknown => {
  if (Array.isArray(value)) {
    return value.map(canonical)
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value as Record<string, unknown>)
        .filter((key) => (value as Record<string, unknown>)[key] !== undefined)
        .sort()
        .map((key) => [key, canonical((value as Record<string, unknown>)[key])])
    )
  }
  return value
}

export const stableStringify = (value: unknown) =>
  JSON.stringify(canonical(value === undefined ? null : value))

export const sameValue = (a: unknown, b: unknown) =>
  stableStringify(a) === stableStringify(b)

export const cloneValue = <T>(value: T): T =>
  value === undefined ? value : (JSON.parse(JSON.stringify(value)) as T)

const isBlank = (value: unknown) =>
  value === null ||
  value === undefined ||
  (typeof value === 'string' && value.trim() === '')

// The snapshot is the shape the validators accept back: an empty optional
// member is absent, except a token-enum member, which stays as "" — titles
// sent without lang were refused with "element 3: lang must be a string".
const normalizeRow = (members: CatalogSchemaMember[], row: unknown) => {
  if (!row || typeof row !== 'object' || Array.isArray(row)) {
    return row
  }
  const byKey = new Map(members.map((m) => [m.key, m]))
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(row as Record<string, unknown>)) {
    if (key.startsWith('_')) {
      continue
    }
    const member = byKey.get(key)
    if (member?.nullable && isBlank(value)) {
      if (member.type === 'enum') {
        out[key] = ''
      }
      continue
    }
    out[key] = value
  }
  for (const member of members) {
    if (member.type === 'enum' && member.nullable && !(member.key in out)) {
      out[member.key] = ''
    }
  }
  return out
}

export const normalizeFieldValue = (
  field: CatalogSchemaField,
  value: unknown
): unknown => {
  if (field.field_type !== 'list' || !Array.isArray(value)) {
    return value
  }
  const element = field.element
  if (element?.type === 'object') {
    return value.map((row) => normalizeRow(element.members ?? [], row))
  }
  if (element?.type === 'text') {
    return value
      .map((item) => (typeof item === 'string' ? item.trim() : item))
      .filter((item) => !isBlank(item))
  }
  return value
}

export const changedFieldKeys = (
  fields: CatalogSchemaField[],
  base: EditFieldValues,
  draft: EditFieldValues
) =>
  fields
    .filter(
      (field) =>
        !field.deprecated &&
        !sameValue(
          normalizeFieldValue(field, base[field.key]),
          normalizeFieldValue(field, draft[field.key])
        )
    )
    .map((field) => field.key)

export const buildPatch = (
  fields: CatalogSchemaField[],
  base: EditFieldValues,
  draft: EditFieldValues
) => {
  const byKey = new Map(fields.map((field) => [field.key, field]))
  return Object.fromEntries(
    changedFieldKeys(fields, base, draft).map((key) => [
      key,
      normalizeFieldValue(byKey.get(key)!, draft[key])
    ])
  )
}

const asRecord = (row: unknown) =>
  row && typeof row === 'object' ? (row as Record<string, unknown>) : {}

const str = (value: unknown) =>
  value === null || value === undefined ? '' : String(value)

const ROW_IDENTITY: Record<string, (row: Record<string, unknown>) => string> = {
  titles: (row) =>
    workTitleIdentity(Number(row.kind ?? 0), str(row.lang), str(row.title)),
  intros: (row) => str(row.lang),
  labels: (row) => `${str(row.label_id)}:${str(row.kind)}`,
  covers: (row) => str(row.image_hash),
  screenshots: (row) => str(row.image_hash),
  covers_upstream: (row) => str(row.image_hash),
  screenshots_upstream: (row) => str(row.image_hash),
  credits: (row) =>
    `${str(row.role_id)}:${str(row.credit_name_id)}:${str(row.character_id) || '0'}`,
  roster: (row) => str(row.character_id),
  traits: (row) => str(row.trait_id),
  relations: (row) => `${str(row.relation)}:${str(row.label_id)}`,
  aliases: (row) => `${str(row.kind)}:${str(row.lang)}:${str(row.name)}`
}

export const rowIdentity = (fieldKey: string, row: unknown) => {
  if (row === null || typeof row !== 'object') {
    return str(row)
  }
  const rule = ROW_IDENTITY[bareFieldName(fieldKey)]
  return rule ? rule(asRecord(row)) : stableStringify(row)
}

export interface EditVocabSlot {
  vocabulary: string
  encoding: 'int' | 'token'
  base: number
}

export const fieldVocabSlot = (
  field: CatalogSchemaField
): EditVocabSlot | null =>
  field.vocabulary
    ? {
        vocabulary: field.vocabulary,
        encoding: field.encoding === 'int' ? 'int' : 'token',
        base: field.base ?? 0
      }
    : null

export const memberVocabSlot = (
  member: CatalogSchemaMember
): EditVocabSlot | null =>
  member.vocabulary
    ? {
        vocabulary: member.vocabulary,
        encoding: member.type === 'int' ? 'int' : 'token',
        base: member.base ?? 0
      }
    : null

export const vocabWireValues = (slot: EditVocabSlot, vocab: EditVocabulary) =>
  vocab.values.map((entry, index) => ({
    token: entry.value,
    wire: slot.encoding === 'int' ? slot.base + index : entry.value
  }))

export const vocabTokenOf = (
  slot: EditVocabSlot,
  vocab: EditVocabulary | undefined,
  wire: unknown
) => {
  if (wire === null || wire === undefined || wire === '') {
    return null
  }
  if (slot.encoding === 'token') {
    return String(wire)
  }
  return vocab?.values[Number(wire) - slot.base]?.value ?? null
}

export const vocabularyNamesOf = (fields: CatalogSchemaField[]) => {
  const names = new Set<string>()
  for (const field of fields) {
    if (field.vocabulary) {
      names.add(field.vocabulary)
    }
    for (const member of field.element?.members ?? []) {
      if (member.vocabulary) {
        names.add(member.vocabulary)
      }
    }
  }
  return [...names].sort()
}

export const reconcileTitleSuppression = (
  fieldKey: string,
  baseTitles: unknown,
  draftTitles: unknown,
  draftSuppressed: unknown
) => {
  const keys = Array.isArray(draftSuppressed) ? draftSuppressed.map(String) : []
  const present = new Set(
    (Array.isArray(draftTitles) ? draftTitles : []).map((row) =>
      rowIdentity(fieldKey, row)
    )
  )
  const next = keys.filter((key) => !present.has(key))
  for (const row of Array.isArray(baseTitles) ? baseTitles : []) {
    const identity = rowIdentity(fieldKey, row)
    if (!present.has(identity) && !next.includes(identity)) {
      next.push(identity)
    }
  }
  return next
}

export const fingerprint = (value: unknown) => {
  const text = stableStringify(value)
  let hash = 0x811c9dc5
  for (let i = 0; i < text.length; i++) {
    hash ^= text.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return (hash >>> 0).toString(16).padStart(8, '0')
}
