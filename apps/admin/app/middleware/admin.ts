export default defineNuxtRouteMiddleware(async () => {
  const auth = useAuth()

  if (!auth.user.value) {
    await auth.fetchUser()
  }

  if (!auth.user.value) {
    return navigateTo('/auth/login')
  }

  if (!auth.isAdmin.value) {
    return navigateTo('/profile')
  }
})
