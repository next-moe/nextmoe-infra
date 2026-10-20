const MUTATING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])
const RELOAD_GUARD_KEY = 'nextmoe-editor-reauth-at'
const RELOAD_GUARD_MS = 30_000

const reauthenticate = () => {
  try {
    const last = Number(sessionStorage.getItem(RELOAD_GUARD_KEY) ?? 0)
    if (Date.now() - last < RELOAD_GUARD_MS) {
      return
    }
    sessionStorage.setItem(RELOAD_GUARD_KEY, String(Date.now()))
  } catch {
    // sessionStorage can be unavailable; reload anyway.
  }
  window.location.reload()
}

const RETRY_AFTER_CAP_SECONDS = 15

const retryDelayMs = (response: Response | undefined) => {
  if (response?.status !== 429) {
    return 400
  }
  const seconds = Number(response.headers.get('Retry-After'))
  return (
    Math.min(
      Number.isFinite(seconds) && seconds > 0 ? seconds : 2,
      RETRY_AFTER_CAP_SECONDS
    ) *
      1000 +
    Math.floor(Math.random() * 400)
  )
}

const editorFetch = $fetch.create({
  baseURL: '/api',
  retryDelay: ({ response }) => retryDelayMs(response),
  onRequest: ({ options }) => {
    const method = String(options.method ?? 'GET').toUpperCase()
    if (!MUTATING_METHODS.has(method) && options.retry === undefined) {
      options.retry = 2
    }
    if (MUTATING_METHODS.has(method)) {
      const headers = new Headers(options.headers as HeadersInit | undefined)
      headers.set(EDITOR_CSRF_HEADER, '1')
      options.headers = headers
    }
  },
  onResponseError: ({ response }) => {
    if (response.status === 401) {
      reauthenticate()
    }
  }
})

export const useEditorApi = () => editorFetch
