import { useOAuthLogin } from './useOAuthLogin'

export const useAuth = () => {
  const api = useApi()
  const userStore = useUserStore()
  const { startLogin } = useOAuthLogin()

  const accessToken = useCookie('access_token', {
    maxAge: 60 * 15, // 15 minutes
    sameSite: 'lax',
    secure: !import.meta.dev
  })

  const authMode = useCookie('auth_mode', {
    maxAge: 60 * 60 * 24 * 90,
    sameSite: 'lax',
    secure: !import.meta.dev
  })

  const setAccessToken = (token: string) => {
    accessToken.value = token
  }

  const clearAuth = () => {
    accessToken.value = null
    authMode.value = null
    userStore.clearUser()
  }

  const logout = async () => {
    try {
      await $fetch('/auth/logout', { method: 'POST', credentials: 'include' })
    } finally {
      clearAuth()
      if (import.meta.client) {
        await startLogin('/')
      }
    }
  }

  const refreshAccessToken = async () => {
    const token = await requestTokenRefresh()
    if (typeof token === 'string') {
      setAccessToken(token)
      return true
    }
    return false
  }

  const fetchUser = async () => {
    if (!accessToken.value) {
      const refreshed = await refreshAccessToken()
      if (!refreshed) return null
    }

    const response = await api.get<User>('/auth/me')
    if (response.code === 0) {
      userStore.setUser(response.data)
      return response.data
    }
    return null
  }

  return {
    user: computed(() => userStore.user),
    isLoggedIn: computed(() => userStore.isLoggedIn),
    isAdmin: computed(() => userStore.isAdmin),
    isRen: computed(() => userStore.isRen),
    setAccessToken,
    logout,
    fetchUser,
    refreshAccessToken
  }
}
