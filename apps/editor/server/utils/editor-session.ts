import type { H3Event } from 'h3'
import type { OAuthTokenSet, TokenResult } from './oauth-tokens'

export const SESSION_COOKIE = {
  access: 'ne_at',
  refresh: 'ne_rt'
} as const

const LOGIN_COOKIE_PREFIX = 'ne_login_'
const LOGIN_COOKIE_PATH = '/auth'
const LOGIN_MAX_AGE = 60 * 10
const REFRESH_MAX_AGE = 60 * 60 * 24 * 90
const ACCESS_FALLBACK_MAX_AGE = 60 * 15

// The OP rotates the refresh token on every use. A page load fires several
// proxied calls at once, each carrying the same cookie; the first refresh wins
// and the rest must reuse its result instead of presenting the spent token.
const refreshOnce = createSingleFlight<TokenResult>(10_000)

const cookieOptions = (maxAge: number, path = '/') => ({
  httpOnly: true,
  secure: !import.meta.dev,
  sameSite: 'lax' as const,
  path,
  maxAge
})

export const landEditorSession = (event: H3Event, tokens: OAuthTokenSet) => {
  setCookie(
    event,
    SESSION_COOKIE.access,
    tokens.access_token,
    cookieOptions(tokens.expires_in || ACCESS_FALLBACK_MAX_AGE)
  )
  if (tokens.refresh_token) {
    setCookie(
      event,
      SESSION_COOKIE.refresh,
      tokens.refresh_token,
      cookieOptions(REFRESH_MAX_AGE)
    )
  }
  event.context.editorAccessToken = tokens.access_token
}

export const clearEditorSession = (event: H3Event) => {
  deleteCookie(event, SESSION_COOKIE.access, { path: '/' })
  deleteCookie(event, SESSION_COOKIE.refresh, { path: '/' })
  event.context.editorAccessToken = undefined
}

export const ensureEditorSession = async (
  event: H3Event
): Promise<string | null> => {
  const cached = event.context.editorAccessToken as string | undefined
  if (cached) {
    return cached
  }
  const access = getCookie(event, SESSION_COOKIE.access)
  if (
    access &&
    isAccessTokenFresh(decodeAccessToken(access), Math.floor(Date.now() / 1000))
  ) {
    event.context.editorAccessToken = access
    return access
  }
  const refresh = getCookie(event, SESSION_COOKIE.refresh)
  if (!refresh) {
    return null
  }
  const config = editorServerConfig(event)
  const result = await refreshOnce(refresh, () =>
    refreshAccessToken(config, refresh)
  )
  if (!result.ok) {
    clearEditorSession(event)
    return null
  }
  landEditorSession(event, result.tokens)
  return result.tokens.access_token
}

export const startSilentLogin = (event: H3Event) => {
  const config = editorServerConfig(event)
  const url = getRequestURL(event)
  const verifier = createCodeVerifier()
  const state = createOAuthState()
  setCookie(
    event,
    `${LOGIN_COOKIE_PREFIX}${state}`,
    JSON.stringify({
      v: verifier,
      r: safeReturnPath(`${url.pathname}${url.search}`)
    }),
    cookieOptions(LOGIN_MAX_AGE, LOGIN_COOKIE_PATH)
  )
  setResponseHeader(event, 'cache-control', 'no-store')
  return sendRedirect(
    event,
    buildAuthorizeUrl(config, {
      state,
      codeChallenge: codeChallengeFor(verifier),
      loginHint: url.searchParams.get('login_hint')
    }),
    302
  )
}

export interface LoginAttempt {
  verifier: string
  returnPath: string
}

export const takeLoginAttempt = (
  event: H3Event,
  state: unknown
): LoginAttempt | null => {
  if (!isOAuthState(state)) {
    return null
  }
  const name = `${LOGIN_COOKIE_PREFIX}${state}`
  const raw = getCookie(event, name)
  deleteCookie(event, name, { path: LOGIN_COOKIE_PATH })
  if (!raw) {
    return null
  }
  try {
    const parsed = JSON.parse(raw) as { v?: unknown; r?: unknown }
    if (typeof parsed.v !== 'string' || parsed.v.length < 43) {
      return null
    }
    return { verifier: parsed.v, returnPath: safeReturnPath(parsed.r) }
  } catch {
    return null
  }
}
