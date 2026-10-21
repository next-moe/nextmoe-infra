import type { InjectionKey } from 'vue'
import type {
  CatalogMergeCandidateList,
  CatalogMergePreview,
  CatalogMergeRequest,
  CatalogMergeUndoReport
} from '#shared/types/catalog'
import type { EditFamily } from '~/constants/editEntities'
import {
  choicesBody,
  initialChoices,
  sameChoices,
  type MergeChoices,
  type MergeSide
} from '~/utils/merge/plan'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

export type MergeWorkbenchMode = 'compose' | 'request'
export type MergeDecision = 'approve' | 'reject' | 'execute' | 'undo'

const LIVE_STATES = new Set(['open', 'approved'])

export const createMergeWorkbench = (options: {
  mode: MergeWorkbenchMode
  family: Ref<EditFamily>
  source: Ref<string | null>
  target: Ref<string | null>
  requestId?: string
}) => {
  const api = useEditorApi()
  const { isReviewer, session } = useEditorSession()

  const request = ref<CatalogMergeRequest | null>(null)
  const requestLoading = ref(options.mode === 'request')
  const requestError = ref('')
  const undoReport = ref<CatalogMergeUndoReport | null>(null)

  const sourceId = computed(() =>
    options.mode === 'request'
      ? (request.value?.source_id ?? null)
      : options.source.value
  )
  const targetId = computed(() =>
    options.mode === 'request'
      ? (request.value?.target_id ?? null)
      : options.target.value
  )

  const previewWanted = computed(
    () =>
      !!sourceId.value &&
      !!targetId.value &&
      (options.mode === 'compose' ||
        LIVE_STATES.has(request.value?.state ?? ''))
  )

  const preview = ref<CatalogMergePreview | null>(null)
  const previewLoading = ref(false)
  const previewError = ref('')
  const choices = ref<MergeChoices>({ fields: {}, hide: [] })
  const savedChoices = ref<MergeChoices>({ fields: {}, hide: [] })

  let previewToken = 0
  const loadPreview = async () => {
    if (!previewWanted.value) {
      preview.value = null
      return
    }
    const mine = ++previewToken
    previewLoading.value = true
    previewError.value = ''
    try {
      const result = await api<CatalogMergePreview>('/v2/me/merge-preview', {
        query: {
          object: options.family.value,
          source_id: sourceId.value,
          target_id: targetId.value
        }
      })
      if (mine !== previewToken) {
        return
      }
      preview.value = result
      const stored = request.value
        ? {
            field_resolution: request.value.field_resolution,
            hide_release_ids: request.value.hide_release_ids
          }
        : null
      choices.value = initialChoices(result, stored)
      savedChoices.value = initialChoices(result, stored)
    } catch (error) {
      if (mine === previewToken) {
        preview.value = null
        previewError.value = problemMessage(error, '无法计算合并预览')
      }
    } finally {
      if (mine === previewToken) {
        previewLoading.value = false
      }
    }
  }

  const loadRequest = async () => {
    if (!options.requestId) {
      return
    }
    requestLoading.value = true
    requestError.value = ''
    try {
      request.value = await api<CatalogMergeRequest>(
        `/v2/me/merge-requests/${options.requestId}`
      )
      options.family.value = (request.value.target_object ??
        'work') as EditFamily
    } catch (error) {
      requestError.value = problemMessage(error, '无法读取这条合并请求')
    } finally {
      requestLoading.value = false
    }
  }

  watch([sourceId, targetId, previewWanted], loadPreview)

  const candidates = ref<CatalogMergeCandidateList['items']>([])
  const anchorId = computed(() => sourceId.value ?? targetId.value)
  watch(
    [anchorId, () => options.family.value],
    async ([id]) => {
      candidates.value = []
      if (options.mode !== 'compose' || !id) {
        return
      }
      const list = await api<CatalogMergeCandidateList>(
        '/v2/me/merge-candidates',
        { query: { object: options.family.value, id } }
      ).catch(() => null)
      candidates.value = (list?.items ?? []).filter(
        (item) => item.id !== sourceId.value && item.id !== targetId.value
      )
    },
    { immediate: true }
  )

  const isRequester = computed(
    () =>
      !!request.value &&
      String(request.value.proposed_by_uid) === String(session.value?.id)
  )
  const canEditChoices = computed(() =>
    options.mode === 'compose'
      ? true
      : isReviewer.value && LIVE_STATES.has(request.value?.state ?? '')
  )
  const dirty = computed(
    () =>
      options.mode === 'request' &&
      !!preview.value &&
      !sameChoices(preview.value, choices.value, savedChoices.value)
  )
  const blocked = computed(() => (preview.value?.blockers.length ?? 0) > 0)

  const choose = (key: string, side: MergeSide) => {
    if (canEditChoices.value) {
      choices.value = {
        ...choices.value,
        fields: { ...choices.value.fields, [key]: side }
      }
    }
  }
  const toggleHide = (releaseId: string, hidden: boolean) => {
    if (!canEditChoices.value) {
      return
    }
    const rest = choices.value.hide.filter((id) => id !== releaseId)
    choices.value = {
      ...choices.value,
      hide: hidden ? [...rest, releaseId] : rest
    }
  }
  const setHideAll = (ids: string[], hidden: boolean) => {
    if (!canEditChoices.value) {
      return
    }
    const rest = choices.value.hide.filter((id) => !ids.includes(id))
    choices.value = {
      ...choices.value,
      hide: hidden ? [...rest, ...ids] : rest
    }
  }

  const busy = ref<string | null>(null)

  const submit = async (reason: string) => {
    if (!preview.value || busy.value) {
      return null
    }
    busy.value = 'submit'
    try {
      const created = await api<CatalogMergeRequest>('/v2/me/merge-requests', {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: {
          object: options.family.value,
          source_id: sourceId.value,
          target_id: targetId.value,
          reason: reason.trim(),
          ...choicesBody(preview.value, choices.value)
        }
      })
      return created
    } catch (error) {
      useKunMessage(problemMessage(error, '提交合并请求失败'), 'error')
      return null
    } finally {
      busy.value = null
    }
  }

  const saveChoices = async () => {
    if (!preview.value || !request.value || !dirty.value) {
      return true
    }
    busy.value = 'amend'
    try {
      request.value = await api<CatalogMergeRequest>(
        `/v2/moderation/merge-requests/${request.value.id}`,
        {
          method: 'PATCH',
          body: choicesBody(preview.value, choices.value)
        }
      )
      savedChoices.value = { ...choices.value, hide: [...choices.value.hide] }
      return true
    } catch (error) {
      useKunMessage(problemMessage(error, '保存修改失败'), 'error')
      return false
    } finally {
      busy.value = null
    }
  }

  const decide = async (decision: MergeDecision, reason = '') => {
    if (!request.value || busy.value) {
      return false
    }
    if (decision === 'approve' && !(await saveChoices())) {
      return false
    }
    busy.value = decision
    try {
      const updated = await api<
        CatalogMergeRequest & { undo_report?: CatalogMergeUndoReport }
      >(`/v2/moderation/merge-requests/${request.value.id}/decisions`, {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: { decision, ...(reason.trim() ? { reason: reason.trim() } : {}) }
      })
      request.value = updated
      undoReport.value = updated.undo_report ?? null
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
      request.value = await api<CatalogMergeRequest>(
        `/v2/me/merge-requests/${request.value.id}`,
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

  const resetChoices = () => {
    choices.value = {
      fields: { ...savedChoices.value.fields },
      hide: [...savedChoices.value.hide]
    }
  }

  const ready = (async () => {
    await loadRequest()
    await loadPreview()
  })()

  return {
    mode: options.mode,
    family: options.family,
    sourceId,
    targetId,
    request,
    requestLoading,
    requestError,
    preview,
    previewLoading,
    previewError,
    candidates,
    choices,
    dirty,
    blocked,
    busy,
    undoReport,
    isReviewer,
    isRequester,
    canEditChoices,
    ready,
    choose,
    toggleHide,
    setHideAll,
    resetChoices,
    submit,
    saveChoices,
    decide,
    withdraw,
    reload: loadPreview
  }
}

export type MergeWorkbench = ReturnType<typeof createMergeWorkbench>

const KEY: InjectionKey<MergeWorkbench> = Symbol('merge-workbench')

export const provideMergeWorkbench = (workbench: MergeWorkbench) =>
  provide(KEY, workbench)

export const useMergeWorkbench = () => {
  const workbench = inject(KEY)
  if (!workbench) {
    throw new Error('useMergeWorkbench outside a merge workbench')
  }
  return workbench
}
