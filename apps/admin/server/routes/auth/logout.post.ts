import {
  clearOAuthSession,
  resolveOAuthApiBase
} from '../../utils/oauth-session'

export default defineEventHandler(async (event) => {
  const refreshToken = getCookie(event, 'refresh_token')
  if (refreshToken) {
    await $fetch(`${resolveOAuthApiBase(event)}/oauth/revoke`, {
      method: 'POST',
      body: { token: refreshToken }
    }).catch(() => {
    })
  }
  clearOAuthSession(event)
  return { code: 0 }
})
