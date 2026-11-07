export default defineEventHandler((event) => {
  const query = getQuery(event)
  const authorizeUrl = beginOAuthLogin(event, { returnPath: query.redirect })
  const { oauthWebBase } = useRuntimeConfig(event).public
  return sendRedirect(
    event,
    `${oauthWebBase}/auth/register?redirect=${encodeURIComponent(authorizeUrl)}`,
    302
  )
})
