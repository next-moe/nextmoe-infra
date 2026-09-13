import {
  generateCodeChallenge,
  generateCodeVerifier,
  generateState
} from '~/utils/oauth-pkce'
import { isSafeInternalPath } from '~/utils/safe-path'

export const useOAuthLogin = () => {
  const config = useRuntimeConfig()

  const buildAuthorizeUrl = async (
    redirect?: string,
    extra?: { prompt?: string; loginHint?: string }
  ): Promise<string> => {
    const verifier = generateCodeVerifier()
    const challenge = await generateCodeChallenge(verifier)
    const state = generateState()

    sessionStorage.setItem('oauth_code_verifier', verifier)
    sessionStorage.setItem('oauth_state', state)
    if (isSafeInternalPath(redirect))
      sessionStorage.setItem('oauth_redirect', redirect)
    else sessionStorage.removeItem('oauth_redirect')

    const params = new URLSearchParams({
      response_type: 'code',
      client_id: config.public.oauthClientId,
      redirect_uri: config.public.oauthRedirectUri,
      scope: 'openid profile email',
      state,
      code_challenge: challenge,
      code_challenge_method: 'S256'
    })
    if (extra?.prompt) params.set('prompt', extra.prompt)
    if (extra?.loginHint) params.set('login_hint', extra.loginHint)
    return `${config.public.oauthAuthorizeBase}/oauth/authorize?${params.toString()}`
  }

  const startLogin = async (
    redirect?: string,
    extra?: { prompt?: string; loginHint?: string }
  ) => {
    window.location.href = await buildAuthorizeUrl(redirect, extra)
  }

  const accountProfileUrl = () => `${config.public.oauthWebBase}/profile`

  return { startLogin, accountProfileUrl }
}
