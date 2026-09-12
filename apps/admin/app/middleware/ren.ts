import { useOAuthLogin } from '../composables/useOAuthLogin'

export default defineNuxtRouteMiddleware(async (to) => {
  const auth = useAuth()
  const { startLogin, accountProfileUrl } = useOAuthLogin()

  if (!auth.user.value) {
    await auth.fetchUser()
  }

  if (!auth.user.value) {
    if (import.meta.server) {
      return
    }
    await startLogin(to.fullPath)
    return abortNavigation()
  }

  if (!auth.isRen.value) {
    return navigateTo(accountProfileUrl(), { external: true })
  }
})
