export default defineEventHandler((event) => {
  const query = getQuery(event)
  return sendRedirect(
    event,
    beginOAuthLogin(event, {
      returnPath: query.redirect,
      prompt: query.prompt,
      loginHint: query.login_hint
    }),
    302
  )
})
