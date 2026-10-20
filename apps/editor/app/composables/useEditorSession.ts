import type { EditorSession } from '#shared/types/session'

type SessionStatus = 'idle' | 'loading' | 'ready' | 'error'

export const useEditorSession = () => {
  const session = useState<EditorSession | null>('editor-session', () => null)
  const status = useState<SessionStatus>('editor-session-status', () => 'idle')

  const load = async () => {
    status.value = 'loading'
    try {
      session.value = await useEditorApi()<EditorSession>('/session')
      status.value = 'ready'
    } catch {
      status.value = 'error'
    }
  }

  const ready = computed(() => status.value === 'ready')
  const settled = computed(
    () => status.value === 'ready' || status.value === 'error'
  )
  const isReviewer = computed(() => session.value?.canReview ?? false)

  const logout = async () => {
    const { redirect } = await useEditorApi()<{ redirect: string }>(
      '/auth/logout',
      {
        baseURL: '/',
        method: 'POST'
      }
    )
    window.location.assign(redirect)
  }

  return { session, status, ready, settled, load, isReviewer, logout }
}
