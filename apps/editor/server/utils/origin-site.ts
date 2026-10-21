import type { H3Event } from 'h3'
import { RETURN_SITES } from '../../app/constants/returnSites'
import { resolveReturnLink } from '../../app/utils/returnLink'

export const ORIGIN_COOKIE = 'ne_origin'
export const ORIGIN_HEADER = 'x-nextmoe-origin-site'
const ORIGIN_MAX_AGE = 60 * 60 * 24 * 30

export const isOriginSite = (value: unknown): value is string =>
  typeof value === 'string' && RETURN_SITES.some((site) => site.key === value)

export const originFromLanding = (from: unknown, target: unknown) =>
  resolveReturnLink(from, target, RETURN_SITES) && isOriginSite(from)
    ? from
    : null

export const rememberOrigin = (event: H3Event) => {
  const query = getQuery(event)
  const site = originFromLanding(query.from, query.return)
  if (!site) {
    return
  }
  setCookie(event, ORIGIN_COOKIE, site, {
    httpOnly: true,
    secure: !import.meta.dev,
    sameSite: 'lax',
    path: '/',
    maxAge: ORIGIN_MAX_AGE
  })
}

export const rememberedOrigin = (event: H3Event) => {
  const value = getCookie(event, ORIGIN_COOKIE)
  return isOriginSite(value) ? value : null
}
