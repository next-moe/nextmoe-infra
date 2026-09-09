import {
  clearOAuthSession,
  landOAuthSession,
  resolveOAuthApiBase,
  tokenWireError,
  tokenWirePayload,
  type TokenWire
} from '../../utils/oauth-session'

export default defineEventHandler(async (event) => {
  const refreshToken = getCookie(event, 'refresh_token')
  if (!refreshToken) {
    setResponseStatus(event, 401)
    return { code: 10003, message: '会话已过期' }
  }

  const config = useRuntimeConfig(event)
  let res: TokenWire
  try {
    res = await $fetch<TokenWire>(
      `${resolveOAuthApiBase(event)}/oauth/token`,
      {
        method: 'POST',
        body: {
          grant_type: 'refresh_token',
          refresh_token: refreshToken,
          client_id: config.public.oauthClientId,
          client_secret: config.oauthClientSecret
        }
      }
    )
  } catch (e) {
    const data = (e as { data?: TokenWire })?.data
    if (!data) {
      setResponseStatus(event, 503)
      return { code: -1, message: '刷新暂时失败' }
    }
    res = data // 4xx with an error body → treated as permanent below.
  }

  const tokens = tokenWirePayload(res)
  if (!tokens) {
    clearOAuthSession(event) // permanent: refresh token dead → force re-login.
    setResponseStatus(event, 401)
    return { code: 10003, message: tokenWireError(res) || '会话已过期' }
  }

  landOAuthSession(event, tokens) // rotation writes the new refresh_token.
  return { code: 0, data: { access_token: tokens.access_token } }
})
