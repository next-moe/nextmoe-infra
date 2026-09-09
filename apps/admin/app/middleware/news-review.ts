import { useOAuthLogin } from '../composables/useOAuthLogin'

// news.review sits in the moderator bundle, so gating this page on isAdmin
// would ship a face stricter than the API behind it — a moderator would hold
// the permission with no way to use it.
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

  const roles = auth.user.value.roles ?? []
  if (!roles.includes('admin') && !roles.includes('moderator')) {
    return navigateTo(accountProfileUrl(), { external: true })
  }
})
