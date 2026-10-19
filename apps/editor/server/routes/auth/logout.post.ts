export default defineEventHandler(async (event) => {
  const csrfOk = passesCsrfCheck({
    method: event.method,
    csrfHeader: getRequestHeader(event, CSRF_HEADER),
    origin: getRequestHeader(event, 'origin'),
    host: getRequestHeader(event, 'host')
  })
  if (!csrfOk) {
    throw createError({ statusCode: 403, statusMessage: 'CSRF check failed' })
  }

  const config = editorServerConfig(event)
  const refresh = getCookie(event, SESSION_COOKIE.refresh)
  if (refresh) {
    await revokeRefreshToken(config, refresh)
  }
  clearEditorSession(event)
  return { redirect: buildEndSessionUrl(config) }
})
