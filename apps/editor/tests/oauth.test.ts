import { createHash } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import {
  codeChallengeFor,
  createCodeVerifier,
  createOAuthState,
  isOAuthState
} from '../server/utils/oauth-pkce'
import {
  EDITOR_OAUTH_SCOPE,
  buildAuthorizeUrl,
  buildEndSessionUrl,
  sanitizeLoginHint
} from '../server/utils/oauth-urls'
import {
  decodeAccessToken,
  isAccessTokenFresh
} from '../server/utils/token-claims'
import { createSingleFlight } from '../server/utils/single-flight'

const endpoints = {
  authorizeBase: 'https://account.nextmoe.com/api/v1',
  clientId: 'nextmoe-editor',
  redirectUri: 'https://editor.nextmoe.com/auth/callback'
}

const jwt = (payload: Record<string, unknown>) =>
  [
    'eyJhbGciOiJIUzI1NiJ9',
    Buffer.from(JSON.stringify(payload)).toString('base64url'),
    'sig'
  ].join('.')

describe('PKCE', () => {
  it('produces a 43-char verifier and the RFC 7636 S256 challenge', () => {
    const verifier = createCodeVerifier()
    expect(verifier).toMatch(/^[A-Za-z0-9_-]{43}$/)
    const expected = createHash('sha256')
      .update(verifier)
      .digest('base64')
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/, '')
    expect(codeChallengeFor(verifier)).toBe(expected)
  })

  it('matches the RFC 7636 appendix B test vector', () => {
    expect(
      codeChallengeFor('dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk')
    ).toBe('E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM')
  })

  it('accepts only its own state format', () => {
    expect(isOAuthState(createOAuthState())).toBe(true)
    expect(isOAuthState('../etc')).toBe(false)
    expect(isOAuthState(undefined)).toBe(false)
    expect(isOAuthState('A'.repeat(32))).toBe(false)
  })
})

describe('authorize URL', () => {
  it('carries the PKCE pair, the scope and the client', () => {
    const url = new URL(
      buildAuthorizeUrl(endpoints, {
        state: 'a'.repeat(32),
        codeChallenge: 'challenge'
      })
    )
    expect(url.origin + url.pathname).toBe(
      'https://account.nextmoe.com/api/v1/oauth/authorize'
    )
    expect(Object.fromEntries(url.searchParams)).toEqual({
      response_type: 'code',
      client_id: 'nextmoe-editor',
      redirect_uri: 'https://editor.nextmoe.com/auth/callback',
      scope: EDITOR_OAUTH_SCOPE,
      state: 'a'.repeat(32),
      code_challenge: 'challenge',
      code_challenge_method: 'S256'
    })
  })

  it('forwards a sane login_hint and drops a hostile one', () => {
    const withHint = new URL(
      buildAuthorizeUrl(endpoints, {
        state: 's',
        codeChallenge: 'c',
        loginHint: 'uuid-123'
      })
    )
    expect(withHint.searchParams.get('login_hint')).toBe('uuid-123')
    expect(sanitizeLoginHint('a\nb')).toBeNull()
    expect(sanitizeLoginHint('x'.repeat(257))).toBeNull()
    expect(sanitizeLoginHint('')).toBeNull()
  })

  it('logs out at the OP end-session endpoint for this client', () => {
    expect(buildEndSessionUrl(endpoints)).toBe(
      'https://account.nextmoe.com/api/v1/oauth/logout?client_id=nextmoe-editor'
    )
  })
})

describe('access token claims', () => {
  it('decodes the claims the session face reads', () => {
    const claims = decodeAccessToken(
      jwt({
        sub: 'u-1',
        id: 7,
        name: '鲲',
        roles: ['moderator'],
        site_roles: ['admin'],
        exp: 100
      })
    )
    expect(claims).toEqual({
      sub: 'u-1',
      id: 7,
      name: '鲲',
      scope: '',
      roles: ['moderator'],
      siteRoles: ['admin'],
      exp: 100
    })
  })

  it('treats garbage and expiry-less tokens as absent', () => {
    expect(decodeAccessToken('nope')).toBeNull()
    expect(decodeAccessToken(jwt({ sub: 'x' }))).toBeNull()
  })

  it('refreshes a minute before expiry', () => {
    const claims = decodeAccessToken(jwt({ exp: 1000 }))
    expect(isAccessTokenFresh(claims, 900)).toBe(true)
    expect(isAccessTokenFresh(claims, 941)).toBe(false)
    expect(isAccessTokenFresh(null, 0)).toBe(false)
  })
})

describe('single flight', () => {
  it('shares one refresh between concurrent callers holding the same token', async () => {
    const once = createSingleFlight<number>(50)
    let calls = 0
    const run = () =>
      new Promise<number>((resolve) => {
        calls++
        setTimeout(() => resolve(calls), 5)
      })
    const results = await Promise.all([
      once('rt', run),
      once('rt', run),
      once('rt', run)
    ])
    expect(results).toEqual([1, 1, 1])
    expect(calls).toBe(1)
    expect(await once('rt', run)).toBe(1)
    await new Promise((resolve) => setTimeout(resolve, 60))
    expect(await once('rt', run)).toBe(2)
  })
})
