export default defineEventHandler((event) => {
  const query = getQuery(event)
  return sendRedirect(
    event,
    beginOAuthLogin(event, { returnPath: query.redirect }),
    302
  )
})
