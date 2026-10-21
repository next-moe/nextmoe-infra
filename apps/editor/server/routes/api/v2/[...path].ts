export default defineEventHandler(async (event) => {
  const v2Path = event.path.slice('/api'.length)
  if (!isAllowedV2Path(v2Path)) {
    throw createError({ statusCode: 404, statusMessage: 'Not Found' })
  }
  const csrfOk = passesCsrfCheck({
    method: event.method,
    csrfHeader: getRequestHeader(event, CSRF_HEADER),
    origin: getRequestHeader(event, 'origin'),
    host: getRequestHeader(event, 'host')
  })
  if (!csrfOk) {
    throw createError({ statusCode: 403, statusMessage: 'CSRF check failed' })
  }

  const token = await ensureEditorSession(event)
  if (!token) {
    return respondSessionExpired(event)
  }

  const headers = new Headers({ authorization: `Bearer ${token}` })
  for (const name of PROXY_REQUEST_HEADERS) {
    const value = getRequestHeader(event, name)
    if (value) {
      headers.set(name, value)
    }
  }
  const origin = rememberedOrigin(event)
  if (origin) {
    headers.set(ORIGIN_HEADER, origin)
  }
  const raw = isMutatingMethod(event.method)
    ? await readRawBody(event, false)
    : undefined
  const body = raw ? new Uint8Array(raw) : undefined

  let upstream: Response
  try {
    upstream = await fetch(
      `${editorServerConfig(event).catalogBase}${v2Path}`,
      {
        method: event.method,
        headers,
        body,
        redirect: 'manual'
      }
    )
  } catch {
    throw createError({ statusCode: 502, statusMessage: 'Catalog unreachable' })
  }

  if (upstream.status === 401) {
    deleteCookie(event, SESSION_COOKIE.access, { path: '/' })
  }
  const out = new Headers({ 'cache-control': 'no-store' })
  for (const name of PROXY_RESPONSE_HEADERS) {
    const value = upstream.headers.get(name)
    if (value) {
      out.set(name, value)
    }
  }
  return sendWebResponse(
    event,
    new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: out
    })
  )
})
