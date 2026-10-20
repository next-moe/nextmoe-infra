export interface DuplicateSuspect {
  id: string
  display_name: string
}

interface ProblemFailure {
  data?: { code?: string; suspects?: DuplicateSuspect[] }
}

export const duplicateSuspects = (
  error: unknown
): DuplicateSuspect[] | null => {
  const body = (error as ProblemFailure | null)?.data
  if (body?.code !== 'DUPLICATE_SUSPECTS') {
    return null
  }
  return body.suspects ?? []
}

const isBlank = (value: unknown) =>
  value === null ||
  value === undefined ||
  (typeof value === 'string' && value.trim() === '') ||
  (Array.isArray(value) && value.length === 0)

export interface CreationInput {
  object: string
  values: Record<string, unknown>
  params: Record<string, unknown>
  note: string
  confirmDuplicates?: boolean
}

export const buildCreationBody = ({
  object,
  values,
  params,
  note,
  confirmDuplicates
}: CreationInput) => {
  const patch = Object.fromEntries(
    Object.entries(values)
      .filter(([, value]) => !isBlank(value))
      .map(([key, value]) => [
        key,
        typeof value === 'string' ? value.trim() : value
      ])
  )
  const cleanParams = Object.fromEntries(
    Object.entries(params).filter(([, value]) => !isBlank(value))
  )
  return {
    object,
    patch,
    ...(Object.keys(cleanParams).length ? { params: cleanParams } : {}),
    ...(note.trim() ? { note: note.trim() } : {}),
    ...(confirmDuplicates ? { confirm_duplicates: true } : {})
  }
}

export const missingRequired = (
  required: readonly string[],
  values: Record<string, unknown>
) => required.filter((key) => isBlank(values[key]))

export const parseRefs = (raw: string) =>
  raw
    .split(/[\s,，]+/)
    .map((entry) => entry.trim())
    .filter((entry) => /^[a-z_]+:[^\s:]+$/i.test(entry))
    .map((entry) => {
      const index = entry.indexOf(':')
      return {
        source: entry.slice(0, index).toLowerCase(),
        external_id: entry.slice(index + 1)
      }
    })

export interface ReleaseDate {
  y: number
  m?: number
  d?: number
}

export const parseReleaseDate = (raw: string): ReleaseDate | null => {
  const match = /^(\d{4})(?:-(\d{1,2})(?:-(\d{1,2}))?)?$/.exec(raw.trim())
  if (!match) {
    return null
  }
  const y = Number(match[1])
  const m = match[2] ? Number(match[2]) : 0
  const d = match[3] ? Number(match[3]) : 0
  if (m > 12 || d > 31 || (d && !m)) {
    return null
  }
  return { y, ...(m ? { m } : {}), ...(d ? { d } : {}) }
}

export interface MergeTiming {
  canExecute: boolean
  waitMs: number
}

export const mergeTiming = (
  state: string,
  executeAfter: string | null | undefined,
  now = Date.now()
): MergeTiming => {
  if (state !== 'approved') {
    return { canExecute: false, waitMs: 0 }
  }
  const at = executeAfter ? Date.parse(executeAfter) : Number.NaN
  if (Number.isNaN(at)) {
    return { canExecute: true, waitMs: 0 }
  }
  const waitMs = Math.max(at - now, 0)
  return { canExecute: waitMs === 0, waitMs }
}
