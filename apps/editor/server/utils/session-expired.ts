import type { H3Event } from 'h3'
import { EDITOR_SESSION_EXPIRED } from '../../shared/utils/session'

export const respondSessionExpired = (event: H3Event) => {
  setResponseStatus(event, 401)
  setResponseHeader(event, 'content-type', 'application/problem+json')
  setResponseHeader(event, 'cache-control', 'no-store')
  return {
    type: 'about:blank',
    title: 'Editor session expired',
    status: 401,
    code: EDITOR_SESSION_EXPIRED
  }
}
