import { createHash, randomBytes } from 'node:crypto'
import type { H3Event } from 'h3'

// The state and the verifier live in a server-set cookie named after the
// state, never in sessionStorage: Via and in-app WebViews drop per-tab
// sessionStorage across the round trip to account.nextmoe.com, and the
// callback then read nothing back and refused the login (forum 28b6b488c,
// letmoe 41f9880 hit the same).
const LOGIN_COOKIE_PREFIX = 'oauth_login_'
const LOGIN_COOKIE_PATH = '/auth'
const LOGIN_MAX_AGE = 60 * 10

const isOAuthState = (value: unknown): value is string =>
  typeof value === 'string' && /^[0-9a-f]{32}$/.test(value)

export interface OAuthLoginOptions {
  returnPath?: unknown
  prompt?: unknown
  loginHint?: unknown
}

export const beginOAuthLogin = (
  event: H3Event,
  options: OAuthLoginOptions = {}
): string => {
  const config = useRuntimeConfig(event)
  const verifier = randomBytes(32).toString('base64url')
  const state = randomBytes(16).toString('hex')
  const returnPath = isSafeInternalPath(options.returnPath)
    ? options.returnPath
    : null

  setCookie(
    event,
    `${LOGIN_COOKIE_PREFIX}${state}`,
    JSON.stringify({ v: verifier, r: returnPath }),
    {
      httpOnly: true,
      secure: !import.meta.dev,
      sameSite: 'lax',
      path: LOGIN_COOKIE_PATH,
      maxAge: LOGIN_MAX_AGE
    }
  )
  setResponseHeader(event, 'cache-control', 'no-store')

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: config.public.oauthClientId,
    redirect_uri: config.public.oauthRedirectUri,
    scope: 'openid profile email',
    state,
    code_challenge: createHash('sha256').update(verifier).digest('base64url'),
    code_challenge_method: 'S256'
  })
  if (options.prompt === 'login') params.set('prompt', 'login')
  if (typeof options.loginHint === 'string' && options.loginHint)
    params.set('login_hint', options.loginHint)
  return `${config.public.oauthAuthorizeBase}/oauth/authorize?${params.toString()}`
}

export interface OAuthLoginAttempt {
  verifier: string
  returnPath: string | null
}

export const takeOAuthLogin = (
  event: H3Event,
  state: unknown
): OAuthLoginAttempt | null => {
  if (!isOAuthState(state)) return null
  const name = `${LOGIN_COOKIE_PREFIX}${state}`
  const raw = getCookie(event, name)
  deleteCookie(event, name, { path: LOGIN_COOKIE_PATH })
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as { v?: unknown; r?: unknown }
    if (typeof parsed.v !== 'string' || parsed.v.length < 43) return null
    return {
      verifier: parsed.v,
      returnPath: isSafeInternalPath(parsed.r) ? parsed.r : null
    }
  } catch {
    return null
  }
}
