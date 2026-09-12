export default defineNuxtPlugin(async (nuxtApp) => {
  const route = useRoute()
  if (route.path === '/auth/callback') {
    return
  }

  const auth = useAuth()
  const accessToken = useCookie('access_token')

  if (accessToken.value && auth.user.value) {
    return
  }

  const user = await auth.fetchUser()

  if (user) {
    nuxtApp.hook('app:mounted', () => {
      refreshNuxtData()
    })
  }
})
