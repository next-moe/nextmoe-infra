// news.review sits in the moderator bundle, so gating this page on isAdmin
// would ship a face stricter than the API behind it — a moderator would hold
// the permission with no way to use it.
export default defineNuxtRouteMiddleware(async () => {
  const auth = useAuth()

  if (!auth.user.value) {
    await auth.fetchUser()
  }

  if (!auth.user.value) {
    return navigateTo('/auth/login')
  }

  const roles = auth.user.value.roles ?? []
  if (!roles.includes('admin') && !roles.includes('moderator')) {
    return navigateTo('/profile')
  }
})
