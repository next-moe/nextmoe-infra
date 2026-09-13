
const CODE_STEP_UP_REQUIRED = 10016

interface SwitchSuccess {
  ok: true
  user: User
}

interface SwitchFailure {
  ok: false
  stepUp?: boolean
}

type SwitchResult = SwitchSuccess | SwitchFailure

export const useAccountSwitch = () => {
  const api = useApi()
  const userStore = useUserStore()

  const accessToken = useCookie('access_token', {
    maxAge: 60 * 15, // 15 minutes — mirror useAuth's cookie options
    sameSite: 'lax',
    secure: !import.meta.dev,
  })
  const auth = useAuth()

  const readToken = (): string => {
    if (!import.meta.client) return accessToken.value ?? ''
    const v = document.cookie.match(/(?:^|;\s*)access_token=([^;]+)/)?.[1]
    return v ? decodeURIComponent(v) : ''
  }

  const listBagSessions = async (): Promise<BagSession[]> => {
    const response = await api.get<{ items: BagSession[] }>('/auth/sessions')
    if (response.code === 0 && response.data) {
      return response.data.items ?? []
    }
    return []
  }

  const switchAccount = async (
    sub: string,
    retried = false
  ): Promise<SwitchResult> => {
    const config = useRuntimeConfig()
    const baseUrl = config.public.apiBase || 'http://127.0.0.1:9277/api/v1'
    const token = readToken()
    try {
      const response = await $fetch<{ code: number; data: LoginResponse }>(
        `${baseUrl}/auth/sessions/switch`,
        {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            ...(token ? { Authorization: `Bearer ${token}` } : {}),
          },
          body: JSON.stringify({ sub }),
          credentials: 'include',
        }
      )
      if (response.code === 0 && response.data) {
        accessToken.value = response.data.access_token
        userStore.setUser(response.data.user)
        await nextTick()
        return { ok: true, user: response.data.user }
      }
      return { ok: false }
    } catch (error: unknown) {
      const fetchError = error as {
        statusCode?: number
        data?: { code?: number }
      }
      if (
        fetchError.statusCode === 401 &&
        fetchError.data?.code === CODE_STEP_UP_REQUIRED
      ) {
        return { ok: false, stepUp: true }
      }
      if (fetchError.statusCode === 401 && !retried) {
        const refreshed = await auth.refreshAccessToken()
        if (refreshed) {
          await nextTick()
          return switchAccount(sub, true)
        }
      }
      return { ok: false }
    }
  }

  const logoutAccount = async (sub: string) => {
    return api.post('/auth/sessions/logout', { sub })
  }

  const logoutAllAccounts = async () => {
    return api.post('/auth/sessions/logout-all')
  }

  return {
    listBagSessions,
    switchAccount,
    logoutAccount,
    logoutAllAccounts,
  }
}
