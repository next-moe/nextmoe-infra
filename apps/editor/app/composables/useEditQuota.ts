import type { EditQuota } from '#shared/types/quota'

export const useEditQuota = () => {
  const api = useEditorApi()
  return useAsyncData('editor-quota', () =>
    api<EditQuota>('/v2/me/edit-quota').catch(() => null)
  )
}
