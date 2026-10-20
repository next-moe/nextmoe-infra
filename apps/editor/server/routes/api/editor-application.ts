export default defineEventHandler(async (event) => {
  if (!accountUpstreamPath(event.method)) {
    throw createError({ statusCode: 405, statusMessage: 'Method Not Allowed' })
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

  let body: { reason: string } | undefined
  if (isMutatingMethod(event.method)) {
    const parsed = sanitizeApplicationBody(
      await readBody(event).catch(() => null)
    )
    if (!parsed) {
      setResponseStatus(event, 400)
      return {
        code: -1,
        message: `申请理由需要 ${APPLICATION_REASON_MIN}–${APPLICATION_REASON_MAX} 个字`
      }
    }
    body = parsed
  }

  const token = await ensureEditorSession(event)
  if (!token) {
    return respondSessionExpired(event)
  }

  const result = await forwardAccountRequest({
    fetchImpl: fetch,
    base: editorServerConfig(event).tokenBase,
    token,
    method: event.method,
    body
  })
  if (result.sessionExpired) {
    deleteCookie(event, SESSION_COOKIE.access, { path: '/' })
    return respondSessionExpired(event)
  }
  setResponseHeader(event, 'cache-control', 'no-store')
  setResponseStatus(event, result.status)
  return result.body
})
