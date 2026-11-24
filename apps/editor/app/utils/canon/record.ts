import type {
  CanonEditChange,
  CanonProblemCandidate,
  CanonRecord,
  CanonSchemaProperty
} from '#shared/types/canon'

interface CanonFailure {
  data?: {
    code?: string
    detail?: string
    errors?: string[]
    candidates?: CanonProblemCandidate[]
  }
}

export const canonMessage = (error: unknown, fallback = '操作失败，请稍后再试') => {
  const body = (error as CanonFailure | null)?.data
  if (!body) {
    return fallback
  }
  if (body.code === 'VERSION_CONFLICT') {
    return '这条已经被别人改过，请重新载入后再改'
  }
  return body.errors?.length ? body.errors.join('；') : (body.detail ?? fallback)
}

export const canonCandidates = (error: unknown) =>
  (error as CanonFailure | null)?.data?.candidates ?? []

const NUMBER_SHAPES = ['integer', 'ordinal']

export const inputText = (value: unknown) =>
  value === null || value === undefined ? '' : String(value)

/** A typed value for the API, or undefined when the text is not one. */
export const typedValue = (property: CanonSchemaProperty, text: string | boolean) => {
  if (property.shape === 'bool') {
    return text === true || text === 'true'
  }
  const trimmed = String(text).trim()
  if (NUMBER_SHAPES.includes(property.shape)) {
    const n = Number(trimmed)
    return trimmed === '' || !Number.isFinite(n) ? undefined : n
  }
  return trimmed === '' ? undefined : trimmed
}

export const draftOf = (record: CanonRecord) =>
  Object.fromEntries(
    Object.entries(record.values).map(([key, value]) => [
      key,
      typeof value === 'boolean' ? value : inputText(value)
    ])
  ) as Record<string, string | boolean>

/** Only what changed; a field emptied or left unreadable is not sent. */
export const changeFrom = (
  record: CanonRecord,
  properties: CanonSchemaProperty[],
  draft: Record<string, string | boolean>
): CanonEditChange | null => {
  const before = draftOf(record)
  const values: Record<string, unknown> = {}
  for (const p of properties) {
    if (p.immutable || draft[p.key] === undefined || draft[p.key] === before[p.key]) {
      continue
    }
    const value = typedValue(p, draft[p.key]!)
    if (value !== undefined) {
      values[p.key] = value
    }
  }
  if (!Object.keys(values).length) {
    return null
  }
  return {
    entity: record.entity,
    id: record.id,
    expect_version: record.version,
    values
  }
}
