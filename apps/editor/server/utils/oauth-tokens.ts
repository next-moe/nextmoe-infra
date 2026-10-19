import type { EditorServerConfig } from './editor-config'

export interface OAuthTokenSet {
  access_token: string
  refresh_token?: string
  expires_in?: number
}

export type TokenResult =
  { ok: true; tokens: OAuthTokenSet } | { ok: false; error: string }

const postToken = async (
  config: EditorServerConfig,
  body: Record<string, string>
): Promise<TokenResult> => {
  try {
    const res = await fetch(`${config.tokenBase}/oauth/token`, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        accept: 'application/json'
      },
      body: JSON.stringify(body)
    })
    const data = (await res.json().catch(() => null)) as
      | (Partial<OAuthTokenSet> & {
          error?: string
          error_description?: string
        })
      | null
    if (!res.ok || !data?.access_token) {
      return {
        ok: false,
        error:
          data?.error_description ||
          data?.error ||
          `token endpoint answered ${res.status}`
      }
    }
    return { ok: true, tokens: data as OAuthTokenSet }
  } catch (err) {
    return {
      ok: false,
      error: err instanceof Error ? err.message : 'token endpoint unreachable'
    }
  }
}

export const exchangeAuthorizationCode = (
  config: EditorServerConfig,
  code: string,
  codeVerifier: string
) =>
  postToken(config, {
    grant_type: 'authorization_code',
    code,
    redirect_uri: config.redirectUri,
    client_id: config.clientId,
    code_verifier: codeVerifier
  })

export const refreshAccessToken = (
  config: EditorServerConfig,
  refreshToken: string
) =>
  postToken(config, {
    grant_type: 'refresh_token',
    refresh_token: refreshToken,
    client_id: config.clientId
  })

export const revokeRefreshToken = async (
  config: EditorServerConfig,
  token: string
) => {
  await fetch(`${config.tokenBase}/oauth/revoke`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      token,
      token_type_hint: 'refresh_token',
      client_id: config.clientId
    })
  }).catch(() => undefined)
}
