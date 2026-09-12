
export const REFRESH_TRANSIENT = Symbol('refresh-transient')
export type RefreshResult = string | typeof REFRESH_TRANSIENT | null

const doRefresh = async (): Promise<RefreshResult> => {
  try {
    const response = await $fetch<{
      code: number
      data?: { access_token: string }
    }>('/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include' // browser sends the httpOnly refresh_token cookie
    })
    if (response.code === 0 && response.data) {
      return response.data.access_token
    }
    return null
  } catch (e) {
    const status = (e as { statusCode?: number }).statusCode
    return !status || status >= 500 ? REFRESH_TRANSIENT : null
  }
}

export const requestTokenRefresh = (): Promise<RefreshResult> => {
  const nuxtApp = useNuxtApp()
  const slot = nuxtApp as unknown as {
    _authRefreshInFlight?: Promise<RefreshResult>
  }
  if (!slot._authRefreshInFlight) {
    slot._authRefreshInFlight = doRefresh().finally(() => {
      slot._authRefreshInFlight = undefined
    })
  }
  return slot._authRefreshInFlight
}
