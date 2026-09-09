export default defineNuxtRouteMiddleware(async () => {
  const auth = useAuth()

  if (!auth.user.value) {
    await auth.fetchUser()
  }

  if (!auth.user.value) {
    return navigateTo('/auth/login')
  }

  if (!auth.isRen.value) {
    return navigateTo('/profile')
  }
})
