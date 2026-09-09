export default defineNuxtRouteMiddleware(async (to) => {
  const accessToken = useCookie('access_token')

  const publicRoutes = ['/auth/login', '/auth/register', '/auth/forgot-password', '/auth/reset-password', '/oauth/authorize']

  if (publicRoutes.includes(to.path)) {
    if (accessToken.value && !to.query.redirect) {
      const auth = useAuth()
      if (!auth.user.value) {
        await auth.fetchUser()
      }
      return navigateTo(auth.isAdmin.value ? '/' : '/profile')
    }
    return
  }

  if (accessToken.value) {
    return
  }

  if (import.meta.server) {
    return
  }

  const auth = useAuth()
  const refreshed = await auth.refreshAccessToken()
  if (!refreshed) {
    return navigateTo('/auth/login')
  }
})
