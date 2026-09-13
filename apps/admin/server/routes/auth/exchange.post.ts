import {
  landOAuthSession,
  resolveOAuthApiBase,
  tokenWireError,
  tokenWirePayload,
  type TokenWire
} from '../../utils/oauth-session'

export default defineEventHandler(async (event) => {
  const body = await readBody<{ code?: string; code_verifier?: string }>(event)
  if (!body?.code || !body?.code_verifier) {
    setResponseStatus(event, 400)
    return { code: 8, message: '缺少授权码' }
  }

  const config = useRuntimeConfig(event)
  const res = await $fetch<TokenWire>(
    `${resolveOAuthApiBase(event)}/oauth/token`,
    {
      method: 'POST',
      body: {
        grant_type: 'authorization_code',
        code: body.code,
        redirect_uri: config.public.oauthRedirectUri,
        client_id: config.public.oauthClientId,
        client_secret: config.oauthClientSecret,
        code_verifier: body.code_verifier
      }
    }
  ).catch(
    (e: { data?: TokenWire }): TokenWire =>
      e?.data ?? { error: 'server_error', error_description: '换取令牌失败' }
  )

  const tokens = tokenWirePayload(res)
  if (!tokens) {
    setResponseStatus(event, 400)
    return { code: -1, message: tokenWireError(res) || '登录失败' }
  }

  landOAuthSession(event, tokens)
  return { code: 0, data: { access_token: tokens.access_token } }
})
