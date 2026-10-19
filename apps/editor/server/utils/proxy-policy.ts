import { EDITOR_CSRF_HEADER } from '../../shared/utils/session'

export const V2_ALLOWED_PREFIXES = [
  '/v2/catalog/',
  '/v2/me/',
  '/v2/moderation/',
  '/v2/vocabularies',
  '/v2/problems'
] as const

export const PROXY_REQUEST_HEADERS = [
  'accept',
  'accept-language',
  'content-type',
  'if-match',
  'if-none-match',
  'idempotency-key'
] as const

export const PROXY_RESPONSE_HEADERS = [
  'content-type',
  'etag',
  'last-modified',
  'link',
  'location',
  'retry-after',
  'ratelimit',
  'ratelimit-policy',
  'x-ratelimit-limit',
  'x-ratelimit-remaining',
  'x-ratelimit-reset',
  'x-request-id'
] as const

export const CSRF_HEADER = EDITOR_CSRF_HEADER

export const isAllowedV2Path = (path: string) => {
  const pathname = path.split(/[?#]/, 1)[0] ?? ''
  if (/%2e|%2f|%5c|\\/i.test(pathname)) {
    return false
  }
  if (
    pathname.split('/').some((segment) => segment === '..' || segment === '.')
  ) {
    return false
  }
  return V2_ALLOWED_PREFIXES.some((prefix) =>
    prefix.endsWith('/')
      ? pathname.startsWith(prefix)
      : pathname === prefix || pathname.startsWith(`${prefix}/`)
  )
}

export const isMutatingMethod = (method: string) =>
  !['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase())

export interface CsrfShape {
  method: string
  csrfHeader?: string | null
  origin?: string | null
  host?: string | null
}

export const passesCsrfCheck = ({
  method,
  csrfHeader,
  origin,
  host
}: CsrfShape) => {
  if (!isMutatingMethod(method)) {
    return true
  }
  if (csrfHeader !== '1') {
    return false
  }
  if (!origin) {
    return true
  }
  try {
    return new URL(origin).host === host
  } catch {
    return false
  }
}
