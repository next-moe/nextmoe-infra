
const resolveApiBase = (): string => {
  const config = useRuntimeConfig()
  return (
    (import.meta.server && config.apiBaseSsr
      ? (config.apiBaseSsr as string)
      : config.public.apiBase) || 'http://127.0.0.1:9277/api/v1'
  )
}

const doRefresh = async (): Promise<string | null> => {
  try {
    const response = await $fetch<{ code: number; data?: { access_token: string } }>(
      `${resolveApiBase()}/auth/refresh`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include' // browser sends the httpOnly refresh_token cookie
      }
    )
    if (response.code === 0 && response.data) {
      return response.data.access_token
    }
  } catch {
    // network / expired / no cookie — caller decides what to do with null
  }
  return null
}

export const requestTokenRefresh = (): Promise<string | null> => {
  const nuxtApp = useNuxtApp()
  const slot = nuxtApp as unknown as { _authRefreshInFlight?: Promise<string | null> }
  if (!slot._authRefreshInFlight) {
    slot._authRefreshInFlight = doRefresh().finally(() => {
      slot._authRefreshInFlight = undefined
    })
  }
  return slot._authRefreshInFlight
}
