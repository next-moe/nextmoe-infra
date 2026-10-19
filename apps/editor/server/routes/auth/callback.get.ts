const errorPage = (reason: string, returnPath = '/') =>
  `/auth/error?${new URLSearchParams({ reason, return: returnPath }).toString()}`

export default defineEventHandler(async (event) => {
  setResponseHeader(event, 'cache-control', 'no-store')
  const query = getQuery(event)
  const attempt = takeLoginAttempt(event, query.state)

  if (typeof query.error === 'string') {
    return sendRedirect(
      event,
      errorPage(query.error.slice(0, 64), attempt?.returnPath),
      302
    )
  }
  if (!attempt || typeof query.code !== 'string' || query.code.length === 0) {
    return sendRedirect(event, errorPage('invalid_state'), 302)
  }

  const result = await exchangeAuthorizationCode(
    editorServerConfig(event),
    query.code,
    attempt.verifier
  )
  if (!result.ok) {
    console.warn('[editor] code exchange failed:', result.error)
    return sendRedirect(
      event,
      errorPage('exchange_failed', attempt.returnPath),
      302
    )
  }

  landEditorSession(event, result.tokens)
  return sendRedirect(event, attempt.returnPath, 302)
})
