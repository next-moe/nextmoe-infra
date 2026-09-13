export default defineNuxtPlugin(async (nuxtApp) => {
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
