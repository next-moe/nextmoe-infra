import type { H3Event } from 'h3'


const ACCESS_MAX_AGE = 60 * 15 // 15 minutes (matches expires_in)
const REFRESH_MAX_AGE = 60 * 60 * 24 * 90 // 90 days

export interface OAuthTokens {
  access_token: string
  refresh_token?: string
  expires_in?: number
}

export interface TokenWire {
  access_token?: string
  refresh_token?: string
  expires_in?: number
  error?: string
  error_description?: string
}

export const tokenWirePayload = (
  res: TokenWire | null | undefined
): OAuthTokens | null => (res?.access_token ? (res as OAuthTokens) : null)

export const tokenWireError = (
  res: TokenWire | null | undefined
): string | undefined => res?.error_description ?? res?.error

export const resolveOAuthApiBase = (event: H3Event): string => {
  const config = useRuntimeConfig(event)
  const base =
    (config.apiBaseSsr as string) ||
    (config.public.apiBase as string) ||
    'http://127.0.0.1:9277/api/v1'
  return base.replace(/\/$/, '')
}

export const landOAuthSession = (event: H3Event, tokens: OAuthTokens) => {
  const secure = !import.meta.dev
  setCookie(event, 'access_token', tokens.access_token, {
    path: '/',
    httpOnly: false,
    sameSite: 'lax',
    secure,
    maxAge: tokens.expires_in || ACCESS_MAX_AGE
  })
  if (tokens.refresh_token) {
    setCookie(event, 'refresh_token', tokens.refresh_token, {
      path: '/auth',
      httpOnly: true,
      sameSite: 'lax',
      secure,
      maxAge: REFRESH_MAX_AGE
    })
  }
  setCookie(event, 'auth_mode', 'oauth', {
    path: '/',
    httpOnly: false,
    sameSite: 'lax',
    secure,
    maxAge: REFRESH_MAX_AGE
  })
}

export const clearOAuthSession = (event: H3Event) => {
  deleteCookie(event, 'access_token', { path: '/' })
  deleteCookie(event, 'refresh_token', { path: '/auth' })
  deleteCookie(event, 'auth_mode', { path: '/' })
}
