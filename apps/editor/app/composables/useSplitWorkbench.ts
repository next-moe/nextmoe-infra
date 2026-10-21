import type { InjectionKey } from 'vue'
import type {
  CatalogSplitPreview,
  CatalogSplitRequest
} from '#shared/types/catalog'
import {
  draftBody,
  draftFromPreview,
  draftFromRequest,
  draftProblems,
  previewQuery,
  refKey,
  sameDraft,
  type SplitDraft,
  type SplitSelection
} from '~/utils/split/plan'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

export type SplitWorkbenchMode = 'compose' | 'request'
export type SplitDecision = 'approve' | 'reject' | 'execute' | 'undo'

const LIVE_STATES = new Set(['open', 'approved'])

const emptyDraft = (): SplitDraft => ({
  displayName: '',
  olang: '',
  titles: []
})

export const createSplitWorkbench = (options: {
  mode: SplitWorkbenchMode
  workId: Ref<string | null>
  requestId?: string
}) => {
  const api = useEditorApi()
  const { isReviewer, session } = useEditorSession()

  const request = ref<CatalogSplitRequest | null>(null)
  const requestLoading = ref(options.mode === 'request')
  const requestError = ref('')

  const workId = computed(() =>
    options.mode === 'request'
      ? (request.value?.target_id ?? null)
      : options.workId.value
  )
  const live = computed(
    () =>
      options.mode === 'compose' || LIVE_STATES.has(request.value?.state ?? '')
  )

  const selection = ref<SplitSelection>({ releases: [], promote: [] })
  const draft = ref<SplitDraft>(emptyDraft())
  const draftTouched = ref(false)
  const saved = ref<{ selection: SplitSelection; draft: SplitDraft } | null>(
    null
  )

  const preview = ref<CatalogSplitPreview | null>(null)
  const previewLoading = ref(false)
  const previewError = ref('')

  let token = 0
  const loadPreview = async () => {
    if (!workId.value || !live.value) {
      preview.value = null
      return
    }
    const mine = ++token
    previewLoading.value = true
    previewError.value = ''
    try {
      const result = await api<CatalogSplitPreview>('/v2/me/split-preview', {
        query: previewQuery(workId.value, selection.value, request.value?.id)
      })
      if (mine !== token) {
        return
      }
      preview.value = result
      if (!draftTouched.value && options.mode === 'compose') {
        draft.value = draftFromPreview(result)
      }
    } catch (error) {
      if (mine === token) {
        previewError.value = problemMessage(error, '无法计算拆分预览')
      }
    } finally {
      if (mine === token) {
        previewLoading.value = false
      }
    }
  }

  let timer: ReturnType<typeof setTimeout> | undefined
  const schedulePreview = () => {
    clearTimeout(timer)
    timer = setTimeout(loadPreview, 250)
  }
  watch(
    () => [
      workId.value,
      live.value,
      selection.value.releases.join(','),
      selection.value.promote.join(',')
    ],
    schedulePreview
  )

  const loadRequest = async () => {
    if (!options.requestId) {
      return
    }
    requestLoading.value = true
    requestError.value = ''
    try {
      request.value = await api<CatalogSplitRequest>(
        `/v2/me/split-requests/${options.requestId}`
      )
      adoptRequest(request.value)
    } catch (error) {
      requestError.value = problemMessage(error, '无法读取这条拆分请求')
    } finally {
      requestLoading.value = false
    }
  }

  const adoptRequest = (r: CatalogSplitRequest) => {
    selection.value = {
      releases: [...r.release_ids],
      promote: r.promote_refs.map(refKey)
    }
    draft.value = draftFromRequest(r)
    draftTouched.value = true
    saved.value = {
      selection: {
        releases: [...selection.value.releases],
        promote: [...selection.value.promote]
      },
      draft: draftFromRequest(r)
    }
  }

  const isRequester = computed(
    () =>
      !!request.value &&
      String(request.value.proposed_by_uid) === String(session.value?.id)
  )
  const canEdit = computed(() =>
    options.mode === 'compose'
      ? true
      : isReviewer.value && LIVE_STATES.has(request.value?.state ?? '')
  )
  const dirty = computed(
    () =>
      options.mode === 'request' &&
      !!saved.value &&
      !sameDraft(
        { selection: selection.value, draft: draft.value },
        saved.value
      )
  )
  const problems = computed(() => draftProblems(selection.value, draft.value))
  const blocked = computed(
    () => (preview.value?.blockers.length ?? 0) > 0 || problems.value.length > 0
  )

  const setReleases = (ids: string[]) => {
    if (canEdit.value) {
      selection.value = { ...selection.value, releases: [...new Set(ids)] }
    }
  }
  const togglePromote = (key: string, on: boolean) => {
    if (!canEdit.value) {
      return
    }
    const rest = selection.value.promote.filter((k) => k !== key)
    selection.value = {
      ...selection.value,
      promote: on ? [...rest, key] : rest
    }
  }
  const editDraft = (next: Partial<SplitDraft>) => {
    if (!canEdit.value) {
      return
    }
    draftTouched.value = true
    draft.value = { ...draft.value, ...next }
  }
  const resetDraftToSuggestion = () => {
    if (preview.value && canEdit.value) {
      draft.value = draftFromPreview(preview.value)
      draftTouched.value = false
    }
  }

  const busy = ref<string | null>(null)

  const submit = async (note: string) => {
    if (!workId.value || busy.value) {
      return null
    }
    busy.value = 'submit'
    try {
      return await api<CatalogSplitRequest>('/v2/me/split-requests', {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: {
          object: 'work',
          target_id: workId.value,
          note: note.trim(),
          ...draftBody(selection.value, draft.value)
        }
      })
    } catch (error) {
      useKunMessage(problemMessage(error, '提交拆分请求失败'), 'error')
      return null
    } finally {
      busy.value = null
    }
  }

  const saveAmendment = async () => {
    if (!request.value || !dirty.value) {
      return true
    }
    busy.value = 'amend'
    try {
      request.value = await api<CatalogSplitRequest>(
        `/v2/moderation/split-requests/${request.value.id}`,
        { method: 'PATCH', body: draftBody(selection.value, draft.value) }
      )
      adoptRequest(request.value)
      return true
    } catch (error) {
      useKunMessage(problemMessage(error, '保存修改失败'), 'error')
      return false
    } finally {
      busy.value = null
    }
  }

  const resetAmendment = () => {
    if (saved.value) {
      selection.value = {
        releases: [...saved.value.selection.releases],
        promote: [...saved.value.selection.promote]
      }
      draft.value = {
        ...saved.value.draft,
        titles: saved.value.draft.titles.map((t) => ({ ...t }))
      }
    }
  }

  const decide = async (decision: SplitDecision, reason = '') => {
    if (!request.value || busy.value) {
      return false
    }
    if (decision === 'approve' && !(await saveAmendment())) {
      return false
    }
    busy.value = decision
    try {
      request.value = await api<CatalogSplitRequest>(
        `/v2/moderation/split-requests/${request.value.id}/decisions`,
        {
          method: 'POST',
          headers: { 'Idempotency-Key': newIdempotencyKey() },
          body: {
            decision,
            ...(reason.trim() ? { reason: reason.trim() } : {})
          }
        }
      )
      adoptRequest(request.value)
      await loadPreview()
      return true
    } catch (error) {
      useKunMessage(problemMessage(error, '操作失败'), 'error')
      return false
    } finally {
      busy.value = null
    }
  }

  const withdraw = async () => {
    if (!request.value || busy.value) {
      return false
    }
    busy.value = 'withdraw'
    try {
      request.value = await api<CatalogSplitRequest>(
        `/v2/me/split-requests/${request.value.id}`,
        { method: 'PATCH', body: { state: 'withdrawn' } }
      )
      preview.value = null
      return true
    } catch (error) {
      useKunMessage(problemMessage(error, '撤回失败'), 'error')
      return false
    } finally {
      busy.value = null
    }
  }

  const ready = (async () => {
    await loadRequest()
    await loadPreview()
  })()

  onBeforeUnmount(() => clearTimeout(timer))

  return {
    mode: options.mode,
    workId,
    request,
    requestLoading,
    requestError,
    preview,
    previewLoading,
    previewError,
    selection,
    draft,
    draftTouched,
    dirty,
    problems,
    blocked,
    busy,
    live,
    isReviewer,
    isRequester,
    canEdit,
    ready,
    setReleases,
    togglePromote,
    editDraft,
    resetDraftToSuggestion,
    submit,
    saveAmendment,
    resetAmendment,
    decide,
    withdraw,
    reload: loadPreview
  }
}

export type SplitWorkbench = ReturnType<typeof createSplitWorkbench>

const KEY: InjectionKey<SplitWorkbench> = Symbol('split-workbench')

export const provideSplitWorkbench = (workbench: SplitWorkbench) =>
  provide(KEY, workbench)

export const useSplitWorkbench = () => {
  const workbench = inject(KEY)
  if (!workbench) {
    throw new Error('useSplitWorkbench outside a split workbench')
  }
  return workbench
}
