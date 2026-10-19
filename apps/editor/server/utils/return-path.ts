import { hasControlCharacter } from './control-chars'

const FALLBACK = '/'

export const safeReturnPath = (raw: unknown) => {
  if (typeof raw !== 'string' || raw.length === 0 || raw.length > 2048) {
    return FALLBACK
  }
  if (!raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/\\')) {
    return FALLBACK
  }
  if (hasControlCharacter(raw)) {
    return FALLBACK
  }
  const url = new URL(raw, 'http://editor.invalid')
  if (url.origin !== 'http://editor.invalid') {
    return FALLBACK
  }
  if (url.pathname === '/auth' || url.pathname.startsWith('/auth/')) {
    return FALLBACK
  }
  url.searchParams.delete('login_hint')
  return `${url.pathname}${url.search}${url.hash}`
}
