import {
  REFRESH_TRANSIENT,
  requestTokenRefresh
} from '../composables/useTokenRefresh'
import { useOAuthLogin } from '../composables/useOAuthLogin'

export default defineNuxtRouteMiddleware(async (to) => {
  if (to.path === '/auth/callback') {
    return
  }

  const accessToken = useCookie('access_token')

  if (accessToken.value) {
    return
  }

  if (import.meta.server) {
    return
  }

  const auth = useAuth()
  const { startLogin } = useOAuthLogin()
  const result = await requestTokenRefresh()
  if (typeof result === 'string') {
    auth.setAccessToken(result)
    return
  }

  if (result === REFRESH_TRANSIENT) {
    return
  }

  await startLogin(to.fullPath)
  return abortNavigation()
})
