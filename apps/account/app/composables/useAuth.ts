export const useAuth = () => {
  const api = useApi()
  const userStore = useUserStore()

  const accessToken = useCookie('access_token', {
    maxAge: 60 * 15, // 15 minutes
    sameSite: 'lax',
    secure: !import.meta.dev
  })


  const setAccessToken = (token: string) => {
    accessToken.value = token
  }

  const clearAuth = () => {
    accessToken.value = null
    userStore.clearUser()
  }

  const login = async (account: string, password: string) => {
    const response = await api.post<LoginResponse>('/auth/login', {
      account,
      password,
    })
    if (response.code === 0) {
      setAccessToken(response.data.access_token)
      userStore.setUser(response.data.user)
    }
    return response
  }

  const sendRegisterCode = async (name: string, email: string) => {
    return api.post('/auth/register/send-code', { name, email })
  }

  const register = async (
    name: string,
    email: string,
    password: string,
    code: string
  ) => {
    const response = await api.post<LoginResponse>('/auth/register', {
      name,
      email,
      password,
      code,
    })
    if (response.code === 0) {
      setAccessToken(response.data.access_token)
      userStore.setUser(response.data.user)
    }
    return response
  }

  const completeFederation = async (payload: {
    token: string
    name: string
    password: string
    email?: string
    code?: string
  }) => {
    const response = await api.post<LoginResponse>(
      '/auth/federation/complete',
      {
        token: payload.token,
        name: payload.name,
        password: payload.password,
        email: payload.email,
        code: payload.code,
      }
    )
    if (response.code === 0) {
      setAccessToken(response.data.access_token)
      userStore.setUser(response.data.user)
    }
    return response
  }

  const logout = async () => {
    try {
      await api.post('/auth/logout')
    } finally {
      clearAuth()
      navigateTo('/auth/login')
    }
  }

  const logoutSilent = async () => {
    try {
      await refreshAccessToken()
      await api.post('/auth/logout')
    } catch {
      // ignore — clearAuth below still runs
    } finally {
      clearAuth()
    }
  }

  const refreshAccessToken = async () => {
    const token = await requestTokenRefresh()
    if (token) {
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

  const forgotPassword = async (email: string) => {
    return api.post('/auth/password/forgot', { email })
  }

  const resetPassword = async (token: string, password: string) => {
    return api.post('/auth/password/reset', { token, password })
  }

  const changePassword = async (oldPassword: string, newPassword: string) => {
    return api.put('/auth/password', {
      old_password: oldPassword,
      new_password: newPassword,
    })
  }

  const sendEmailChangeCode = async (newEmail: string) => {
    return api.post('/auth/email/send-code', { new_email: newEmail })
  }

  const changeEmail = async (code: string, newEmail: string) => {
    return api.put('/auth/email', { code, new_email: newEmail })
  }

  const updateProfile = async (payload: {
    name?: string
    bio?: string
    avatar?: string
    avatar_image_hash?: string
  }) => {
    const response = await api.patch<User>('/auth/me', payload)
    if (response.code === 0 && response.data) {
      userStore.setUser(response.data)
    }
    return response
  }

  return {
    user: computed(() => userStore.user),
    isLoggedIn: computed(() => userStore.isLoggedIn),
    isAdmin: computed(() => userStore.isAdmin),
    isRen: computed(() => userStore.isRen),
    login,
    sendRegisterCode,
    register,
    completeFederation,
    logout,
    logoutSilent,
    fetchUser,
    refreshAccessToken,
    forgotPassword,
    resetPassword,
    changePassword,
    sendEmailChangeCode,
    changeEmail,
    updateProfile,
  }
}
