export const useOAuthLogin = () => {
  const config = useRuntimeConfig()

  const startLogin = (
    redirect?: string,
    extra?: { prompt?: string; loginHint?: string }
  ) => {
    const params = new URLSearchParams()
    if (redirect) params.set('redirect', redirect)
    if (extra?.prompt) params.set('prompt', extra.prompt)
    if (extra?.loginHint) params.set('login_hint', extra.loginHint)
    const query = params.toString()
    window.location.href = query ? `/auth/login?${query}` : '/auth/login'
  }

  const accountProfileUrl = () => `${config.public.oauthWebBase}/profile`

  return { startLogin, accountProfileUrl }
}
