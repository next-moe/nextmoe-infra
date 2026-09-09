interface ApiOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE' | 'PATCH'
  body?: Record<string, unknown>
  query?: Record<string, string | number | boolean | undefined>
  headers?: Record<string, string>
}

interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

interface ApiError {
  code: number
  message: string
}

export type ApiService = 'oauth' | 'catalog' | 'trust' | 'ai'

export const resolveApiBase = (service: ApiService = 'oauth'): string => {
  const config = useRuntimeConfig()
  // ("共 0 条候选" hydration mismatch).
  if (service === 'catalog') {
    return (config.public.catalogApiBase as string) || '/catalog-proxy'
  }
  if (service === 'trust') {
    return (config.public.trustApiBase as string) || '/trust-proxy'
  }
  // hydration mismatch); the relay forwards to aiApiBaseSsr server-side.
  if (service === 'ai') {
    return (config.public.aiApiBase as string) || '/ai-proxy'
  }
  return (
    (import.meta.server && config.apiBaseSsr
      ? (config.apiBaseSsr as string)
      : (config.public.apiBase as string)) || 'http://127.0.0.1:9277/api/v1'
  )
}

export const useApi = (service: ApiService = 'oauth') => {
  const baseUrl = resolveApiBase(service)
  const accessToken = useCookie('access_token')

  const getAuthHeaders = (): Record<string, string> => {
    const token = accessToken.value
    return token ? { Authorization: `Bearer ${token}` } : {}
  }

  const handleUnauthorized = async () => {
    const token = await requestTokenRefresh()
    if (token) {
      accessToken.value = token
      return true
    }

    accessToken.value = null
    useUserStore().clearUser()
    if (import.meta.client) {
      const here = window.location.pathname + window.location.search
      if (!here.startsWith('/auth/login')) {
        navigateTo(`/auth/login?redirect=${encodeURIComponent(here)}`)
      }
    }
    return false
  }

  const request = async <T>(
    endpoint: string,
    options: ApiOptions = {},
    retry = true
  ): Promise<ApiResponse<T>> => {
    const { method = 'GET', body, query, headers = {} } = options

    let url = `${baseUrl}${endpoint}`
    if (query) {
      const params = new URLSearchParams()
      for (const [k, v] of Object.entries(query)) {
        if (v !== undefined) params.set(k, String(v))
      }
      const qs = params.toString()
      if (qs) url += `?${qs}`
    }

    try {
      const response = await $fetch<ApiResponse<T>>(url, {
        method,
        body: body ? JSON.stringify(body) : undefined,
        headers: {
          'Content-Type': 'application/json',
          ...getAuthHeaders(),
          ...headers
        },
        credentials: 'include'
      })

      return response
    } catch (error: unknown) {
      const fetchError = error as { statusCode?: number; data?: ApiError }

      if (fetchError.statusCode === 401 && retry) {
        const refreshed = await handleUnauthorized()
        if (refreshed) {
          return request<T>(endpoint, options, false)
        }
      }

      return {
        code: fetchError.data?.code ?? fetchError.statusCode ?? -1,
        message: fetchError.data?.message ?? 'Request failed',
        data: null as T
      }
    }
  }

  return {
    get: <T>(
      endpoint: string,
      query?: Record<string, string | number | boolean | undefined>
    ) => request<T>(endpoint, { method: 'GET', query }),
    post: <T>(endpoint: string, body?: Record<string, unknown>) =>
      request<T>(endpoint, { method: 'POST', body }),
    put: <T>(endpoint: string, body?: Record<string, unknown>) =>
      request<T>(endpoint, { method: 'PUT', body }),
    delete: <T>(
      endpoint: string,
      query?: Record<string, string | number | boolean | undefined>
    ) => request<T>(endpoint, { method: 'DELETE', query }),
    patch: <T>(endpoint: string, body?: Record<string, unknown>) =>
      request<T>(endpoint, { method: 'PATCH', body })
  }
}
