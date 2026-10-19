const NON_PAGE_PREFIXES = ['/auth/', '/api/', '/_nuxt/', '/__nuxt', '/_ipx/']

const STATIC_FILE = /\.[a-z0-9]{1,12}$/i

export interface RequestShape {
  method: string
  path: string
  accept?: string | null
  fetchDest?: string | null
}

export const isPageNavigation = ({
  method,
  path,
  accept,
  fetchDest
}: RequestShape) => {
  if (method !== 'GET' && method !== 'HEAD') {
    return false
  }
  const pathname = path.split(/[?#]/, 1)[0] ?? '/'
  if (
    pathname === '/auth' ||
    NON_PAGE_PREFIXES.some((p) => pathname.startsWith(p))
  ) {
    return false
  }
  if (STATIC_FILE.test(pathname)) {
    return false
  }
  if (fetchDest) {
    return fetchDest === 'document'
  }
  return (accept ?? '').includes('text/html')
}
