import { hasControlCharacter } from './control-chars'

export const EDITOR_OAUTH_SCOPE = 'openid profile catalog:read catalog:edit'

export interface EditorOAuthEndpoints {
  authorizeBase: string
  clientId: string
  redirectUri: string
}

export interface AuthorizeRequest {
  state: string
  codeChallenge: string
  loginHint?: string | null
}

export const sanitizeLoginHint = (raw: unknown) =>
  typeof raw === 'string' &&
  raw.length > 0 &&
  raw.length <= 256 &&
  !hasControlCharacter(raw)
    ? raw
    : null

export const buildAuthorizeUrl = (
  endpoints: EditorOAuthEndpoints,
  { state, codeChallenge, loginHint }: AuthorizeRequest
) => {
  const query = new URLSearchParams({
    response_type: 'code',
    client_id: endpoints.clientId,
    redirect_uri: endpoints.redirectUri,
    scope: EDITOR_OAUTH_SCOPE,
    state,
    code_challenge: codeChallenge,
    code_challenge_method: 'S256'
  })
  const hint = sanitizeLoginHint(loginHint)
  if (hint) {
    query.set('login_hint', hint)
  }
  return `${endpoints.authorizeBase}/oauth/authorize?${query.toString()}`
}

export const buildEndSessionUrl = (endpoints: EditorOAuthEndpoints) =>
  `${endpoints.authorizeBase}/oauth/logout?${new URLSearchParams({ client_id: endpoints.clientId }).toString()}`
