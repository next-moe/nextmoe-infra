import type { UseFetchOptions } from '#app'
import { resolveApiBase, type ApiService } from './useApi'

interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export const useApiFetch = <T>(
  url: string | (() => string),
  options: UseFetchOptions<ApiResponse<T>, T | null> = {},
  service: ApiService = 'oauth'
) => {
  const baseURL = resolveApiBase(service)
  const accessToken = useCookie('access_token')

  return useFetch(url, {
    baseURL,
    // data instead of silently falling back to the caller's empty default.
    retry: 1,
    retryStatusCodes: [401],
    onRequest({ options: requestOptions }) {
      if (accessToken.value) {
        const headers = new Headers(
          requestOptions.headers as HeadersInit | undefined
        )
        headers.set('Authorization', `Bearer ${accessToken.value}`)
        requestOptions.headers = headers
      }
    },
    async onResponseError({ response }) {
      if (import.meta.client && response.status === 401) {
        const token = await requestTokenRefresh()
        if (token) accessToken.value = token
      }
    },
    transform: (resp: ApiResponse<T>) =>
      resp && resp.code === 0 ? resp.data : null,
    ...options,
  })
}
