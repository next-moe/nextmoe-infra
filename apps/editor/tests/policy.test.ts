import { describe, expect, it } from 'vitest'
import { safeReturnPath } from '../server/utils/return-path'
import { isPageNavigation } from '../server/utils/request-kind'
import {
  isAllowedV2Path,
  isMutatingMethod,
  passesCsrfCheck
} from '../server/utils/proxy-policy'
import { resolveReturnLink } from '../app/utils/returnLink'
import { RETURN_SITES } from '../app/constants/returnSites'

describe('return path after login', () => {
  it.each([
    ['/works/4?tab=media', '/works/4?tab=media'],
    ['/', '/'],
    ['/me/proposals#top', '/me/proposals#top']
  ])('keeps the same-origin path %s', (input, expected) => {
    expect(safeReturnPath(input)).toBe(expected)
  })

  it.each([
    ['/works/4/edit?login_hint=abc', '/works/4/edit'],
    [
      '/works/4?login_hint=u%40x.y&from=kungal&return=https%3A%2F%2Fwww.kungal.com%2Fgalgame%2F4',
      '/works/4?from=kungal&return=https%3A%2F%2Fwww.kungal.com%2Fgalgame%2F4'
    ],
    ['/?tab=a&login_hint=z#top', '/?tab=a#top']
  ])(
    'drops login_hint but keeps from/return for the page to store: %s',
    (input, expected) => {
      expect(safeReturnPath(input)).toBe(expected)
    }
  )

  it.each([
    'https://evil.example/x',
    '//evil.example/x',
    '/\\evil.example',
    'javascript:alert(1)',
    '/auth/callback?code=x',
    '/auth',
    '/works\n/4',
    '',
    undefined,
    42
  ])('falls back to / for %s', (input) => {
    expect(safeReturnPath(input)).toBe('/')
  })
})

describe('page navigation detection', () => {
  const nav = (
    path: string,
    extra: Partial<Parameters<typeof isPageNavigation>[0]> = {}
  ) =>
    isPageNavigation({
      method: 'GET',
      path,
      accept: 'text/html,application/xhtml+xml',
      ...extra
    })

  it('treats document requests for app routes as navigations', () => {
    expect(nav('/')).toBe(true)
    expect(nav('/works/4?tab=media')).toBe(true)
    expect(nav('/works', { accept: null, fetchDest: 'document' })).toBe(true)
  })

  it('never intercepts the auth flow, the BFF, build assets or static files', () => {
    expect(nav('/auth/callback?code=x')).toBe(false)
    expect(nav('/auth/error')).toBe(false)
    expect(nav('/api/session')).toBe(false)
    expect(nav('/_nuxt/entry.js')).toBe(false)
    expect(nav('/favicon.ico')).toBe(false)
    expect(nav('/site.webmanifest')).toBe(false)
  })

  it('ignores non-document fetches and writes', () => {
    expect(nav('/works', { accept: 'application/json' })).toBe(false)
    expect(nav('/works', { fetchDest: 'empty' })).toBe(false)
    expect(nav('/works', { method: 'POST' })).toBe(false)
  })
})

describe('v2 proxy allowlist', () => {
  it.each([
    '/v2/catalog/works?limit=1',
    '/v2/me/proposals',
    '/v2/moderation/proposals/9/decisions',
    '/v2/vocabularies',
    '/v2/vocabularies/olang'
  ])('allows %s', (path) => {
    expect(isAllowedV2Path(path)).toBe(true)
  })

  it.each([
    '/v2/store/prices',
    '/v2/folders',
    '/v2/catalog/../me',
    '/v2/catalog/%2e%2e/store',
    '/v2/catalog%2fworks',
    '/v2/vocabulariesx',
    '/api/v1/admin/catalog'
  ])('rejects %s', (path) => {
    expect(isAllowedV2Path(path)).toBe(false)
  })
})

describe('CSRF', () => {
  it('lets reads through untouched', () => {
    expect(isMutatingMethod('GET')).toBe(false)
    expect(passesCsrfCheck({ method: 'GET' })).toBe(true)
  })

  it('requires the custom header and a matching Origin on writes', () => {
    const host = 'editor.nextmoe.com'
    expect(passesCsrfCheck({ method: 'POST', host })).toBe(false)
    expect(passesCsrfCheck({ method: 'POST', csrfHeader: '1', host })).toBe(
      true
    )
    expect(
      passesCsrfCheck({
        method: 'PATCH',
        csrfHeader: '1',
        origin: 'https://editor.nextmoe.com',
        host
      })
    ).toBe(true)
    expect(
      passesCsrfCheck({
        method: 'DELETE',
        csrfHeader: '1',
        origin: 'https://evil.example',
        host
      })
    ).toBe(false)
    expect(
      passesCsrfCheck({ method: 'POST', csrfHeader: '1', origin: 'null', host })
    ).toBe(false)
  })
})

describe('return-to-site link', () => {
  it('accepts an allowlisted https origin for the named site', () => {
    expect(
      resolveReturnLink(
        'kungal',
        'https://www.kungal.com/galgame/4',
        RETURN_SITES
      )
    ).toEqual({
      label: '鲲 Galgame 论坛',
      url: 'https://www.kungal.com/galgame/4'
    })
  })

  it.each([
    ['kungal', 'https://moyu.moe/galgame/4'],
    ['kungal', 'http://www.kungal.com/galgame/4'],
    ['unknown', 'https://www.kungal.com/'],
    ['moyu', 'javascript:alert(1)'],
    ['moyu', undefined]
  ])('rejects %s → %s', (from, target) => {
    expect(resolveReturnLink(from, target, RETURN_SITES)).toBeNull()
  })
})
