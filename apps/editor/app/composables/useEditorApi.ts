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

const editorFetch = $fetch.create({
  baseURL: '/api',
  onRequest: ({ options }) => {
    const method = String(options.method ?? 'GET').toUpperCase()
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
