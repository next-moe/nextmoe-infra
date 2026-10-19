import type { EditorSession } from '#shared/types/session'

export const useEditorSession = () => {
  const session = useState<EditorSession | null>('editor-session', () => null)

  const load = async () => {
    session.value = await useEditorApi()<EditorSession>('/session')
  }

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

  return { session, load, isReviewer, logout }
}
