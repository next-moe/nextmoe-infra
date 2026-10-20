import {
  APPLICATION_REASON_MAX,
  APPLICATION_REASON_MIN
} from '../../shared/utils/application'

export const EDITOR_APPLICATION_ROUTES: Readonly<Record<string, string>> = {
  GET: '/editor/applications/me',
  POST: '/editor/applications'
}

export const accountUpstreamPath = (method: string) =>
  EDITOR_APPLICATION_ROUTES[method.toUpperCase()] ?? null

export const sanitizeApplicationBody = (raw: unknown) => {
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const reason = (raw as { reason?: unknown }).reason
  if (typeof reason !== 'string') {
    return null
  }
  const trimmed = reason.trim()
  const length = [...trimmed].length
  if (length < APPLICATION_REASON_MIN || length > APPLICATION_REASON_MAX) {
    return null
  }
  return { reason: trimmed }
}

interface AccountEnvelope {
  code?: unknown
  message?: unknown
  data?: unknown
}

export interface AccountResult {
  status: number
  body: { data: unknown } | { code: number; message: string }
  sessionExpired: boolean
}

const UNAVAILABLE = '账号服务暂时不可用，请稍后再试'

export const mapAccountResponse = (
  status: number,
  payload: unknown
): AccountResult => {
  const envelope = (
    payload && typeof payload === 'object' ? payload : {}
  ) as AccountEnvelope
  const code = typeof envelope.code === 'number' ? envelope.code : -1
  const message =
    typeof envelope.message === 'string' && envelope.message
      ? envelope.message
      : UNAVAILABLE

  if (status === 401) {
    return {
      status: 401,
      body: { code, message },
      sessionExpired: true
    }
  }
  if (status >= 200 && status < 300 && code === 0) {
    return {
      status: 200,
      body: { data: envelope.data ?? null },
      sessionExpired: false
    }
  }
  if (status >= 400 && status < 500) {
    return { status, body: { code, message }, sessionExpired: false }
  }
  return {
    status: 502,
    body: { code, message: UNAVAILABLE },
    sessionExpired: false
  }
}

export interface ForwardAccountRequest {
  fetchImpl: typeof fetch
  base: string
  token: string
  method: string
  body?: { reason: string }
}

export const forwardAccountRequest = async ({
  fetchImpl,
  base,
  token,
  method,
  body
}: ForwardAccountRequest): Promise<AccountResult> => {
  const path = accountUpstreamPath(method)
  if (!path) {
    return {
      status: 405,
      body: { code: -1, message: 'Method Not Allowed' },
      sessionExpired: false
    }
  }
  const headers: Record<string, string> = {
    authorization: `Bearer ${token}`,
    accept: 'application/json'
  }
  if (body) {
    headers['content-type'] = 'application/json'
  }
  let response: Response
  try {
    response = await fetchImpl(`${base}${path}`, {
      method: method.toUpperCase(),
      headers,
      body: body ? JSON.stringify(body) : undefined,
      redirect: 'manual'
    })
  } catch {
    return mapAccountResponse(502, null)
  }
  const payload = await response.json().catch(() => null)
  return mapAccountResponse(response.status, payload)
}
