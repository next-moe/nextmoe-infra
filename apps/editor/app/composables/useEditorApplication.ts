import type {
  AccountProblem,
  EditorApplication,
  EditorApplicationReply
} from '#shared/types/application'

const problemOf = (error: unknown): AccountProblem => {
  const data = (error as { data?: Partial<AccountProblem> } | null)?.data
  return {
    code: typeof data?.code === 'number' ? data.code : -1,
    message: data?.message || '提交失败，请稍后再试'
  }
}

export const useEditorApplication = () => {
  const api = useEditorApi()
  const application = useState<EditorApplication | null>(
    'editor-application',
    () => null
  )
  const loaded = useState('editor-application-loaded', () => false)
  const loadError = ref<string | null>(null)
  const submitting = ref(false)

  const load = async () => {
    loadError.value = null
    try {
      const reply = await api<EditorApplicationReply>('/editor-application')
      application.value = reply.data
    } catch (error) {
      loadError.value = problemOf(error).message
    } finally {
      loaded.value = true
    }
  }

  const submit = async (reason: string): Promise<AccountProblem | null> => {
    submitting.value = true
    try {
      const reply = await api<EditorApplicationReply>('/editor-application', {
        method: 'POST',
        body: { reason }
      })
      application.value = reply.data
      return null
    } catch (error) {
      return problemOf(error)
    } finally {
      submitting.value = false
    }
  }

  return { application, loaded, loadError, submitting, load, submit }
}
