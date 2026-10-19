interface ProblemBody {
  title?: string
  detail?: string
  code?: string
  errors?: { detail?: string; pointer?: string; reason?: string }[]
}

interface FetchFailure {
  status?: number
  statusCode?: number
  data?: ProblemBody
  response?: { status?: number }
}

export const problemStatus = (error: unknown) => {
  const failure = error as FetchFailure | null
  return (
    failure?.statusCode ?? failure?.status ?? failure?.response?.status ?? 0
  )
}

export const problemMessage = (
  error: unknown,
  fallback = '操作失败，请稍后再试'
) => {
  const body = (error as FetchFailure | null)?.data
  const first = body?.errors?.find((entry) => entry.detail)?.detail
  return (
    [body?.detail || body?.title, first].filter(Boolean).join('：') || fallback
  )
}

export const isStale = (error: unknown) => {
  const status = problemStatus(error)
  return status === 412 || status === 409
}

export const newIdempotencyKey = () =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`
