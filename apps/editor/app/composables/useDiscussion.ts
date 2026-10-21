import type {
  DiscussionPage,
  DiscussionPost,
  DiscussionViewer
} from '#shared/types/discussion'

export interface DiscussionDraft {
  body: string
  replyToId?: string | null
  mentionUids?: string[]
}

export interface PendingDiscussionPost extends DiscussionDraft {
  localId: string
  failed: boolean
}

const PAGE_SIZE = 50

const newLocalId = () =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`

export const useDiscussion = (
  kind: MaybeRefOrGetter<string>,
  anchorId: MaybeRefOrGetter<string>
) => {
  const api = useEditorApi()
  const posts = ref<DiscussionPost[]>([])
  const pending = ref<PendingDiscussionPost[]>([])
  const total = ref(0)
  const viewer = ref<DiscussionViewer | null>(null)
  const ownerUid = ref<string | null>(null)
  const nextCursor = ref<string | null>(null)
  const loading = ref(true)
  const loadingMore = ref(false)
  const loadError = ref('')

  const path = () =>
    `/v2/catalog/discussions/${toValue(kind)}/${toValue(anchorId)}`

  const apply = (page: DiscussionPage, append: boolean) => {
    posts.value = append ? [...posts.value, ...page.items] : page.items
    total.value = page.total
    ownerUid.value = page.owner_uid ?? null
    viewer.value = page.viewer ?? null
    nextCursor.value = page.next_cursor ?? null
  }

  const load = async () => {
    loading.value = true
    loadError.value = ''
    try {
      apply(
        await api<DiscussionPage>(path(), { query: { limit: PAGE_SIZE } }),
        false
      )
    } catch (error) {
      loadError.value = editProblemMessage(parseEditProblem(error))
    } finally {
      loading.value = false
    }
  }

  const loadMore = async () => {
    if (!nextCursor.value || loadingMore.value) {
      return
    }
    loadingMore.value = true
    try {
      apply(
        await api<DiscussionPage>(path(), {
          query: { limit: PAGE_SIZE, cursor: nextCursor.value }
        }),
        true
      )
    } catch (error) {
      useKunMessage(editProblemMessage(parseEditProblem(error)), 'error')
    } finally {
      loadingMore.value = false
    }
  }

  const replacePost = (post: DiscussionPost) => {
    posts.value = posts.value.map((item) => (item.id === post.id ? post : item))
  }

  const deliver = async (item: PendingDiscussionPost) => {
    item.failed = false
    try {
      const post = await api<DiscussionPost>('/v2/me/discussions', {
        method: 'POST',
        headers: { 'Idempotency-Key': item.localId },
        body: {
          anchor_kind: toValue(kind),
          anchor_id: toValue(anchorId),
          body: item.body,
          ...(item.replyToId ? { reply_to_id: item.replyToId } : {}),
          ...(item.mentionUids?.length
            ? { mention_uids: item.mentionUids }
            : {})
        }
      })
      pending.value = pending.value.filter(
        (entry) => entry.localId !== item.localId
      )
      if (!posts.value.some((existing) => existing.id === post.id)) {
        posts.value = [...posts.value, post]
        total.value += 1
      }
      if (viewer.value && !viewer.value.muted) {
        viewer.value = { ...viewer.value, watching: true }
      }
      return true
    } catch (error) {
      item.failed = true
      useKunMessage(editProblemMessage(parseEditProblem(error)), 'error')
      return false
    }
  }

  const send = (draft: DiscussionDraft) => {
    const item = reactive<PendingDiscussionPost>({
      ...draft,
      localId: newLocalId(),
      failed: false
    })
    pending.value = [...pending.value, item]
    return deliver(item)
  }

  const retry = (localId: string) => {
    const item = pending.value.find((entry) => entry.localId === localId)
    return item ? deliver(item) : Promise.resolve(false)
  }

  const discard = (localId: string) => {
    pending.value = pending.value.filter((entry) => entry.localId !== localId)
  }

  const edit = async (postId: string, body: string) => {
    try {
      replacePost(
        await api<DiscussionPost>(`/v2/me/discussions/${postId}`, {
          method: 'PATCH',
          body: { body }
        })
      )
      return true
    } catch (error) {
      useKunMessage(editProblemMessage(parseEditProblem(error)), 'error')
      return false
    }
  }

  const remove = async (postId: string) => {
    try {
      await api(`/v2/me/discussions/${postId}`, { method: 'DELETE' })
      posts.value = posts.value.map((item) =>
        item.id === postId ? { ...item, state: 'deleted', body: '' } : item
      )
      total.value = Math.max(total.value - 1, 0)
      return true
    } catch (error) {
      useKunMessage(editProblemMessage(parseEditProblem(error)), 'error')
      return false
    }
  }

  const moderate = async (postId: string, hidden: boolean, note: string) => {
    try {
      replacePost(
        await api<DiscussionPost>(
          `/v2/moderation/discussions/${postId}/visibility`,
          { method: 'POST', body: { hidden, ...(note ? { note } : {}) } }
        )
      )
      return true
    } catch (error) {
      useKunMessage(editProblemMessage(parseEditProblem(error)), 'error')
      return false
    }
  }

  const setMuted = async (muted: boolean) => {
    try {
      const state = await api<{ watching: boolean; muted: boolean }>(
        '/v2/me/discussion-watches',
        {
          method: 'PUT',
          body: {
            anchor_kind: toValue(kind),
            anchor_id: toValue(anchorId),
            muted
          }
        }
      )
      if (viewer.value) {
        viewer.value = {
          ...viewer.value,
          watching: state.watching,
          muted: state.muted
        }
      }
    } catch (error) {
      useKunMessage(editProblemMessage(parseEditProblem(error)), 'error')
    }
  }

  return {
    posts,
    pending,
    total,
    viewer,
    ownerUid,
    nextCursor,
    loading,
    loadingMore,
    loadError,
    load,
    loadMore,
    send,
    retry,
    discard,
    edit,
    remove,
    moderate,
    setMuted
  }
}
