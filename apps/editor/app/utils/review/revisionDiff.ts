import type { CatalogRevision } from '#shared/types/catalog'
import { useEditorApi } from '~/composables/useEditorApi'
import type { FieldDiffLike } from './history'
import { problemMessage } from './problem'

export interface DiffState {
  loading: boolean
  error: string
  entries: FieldDiffLike[] | null
}

export const diffCacheKey = (revisionId: string, baseId?: string | null) =>
  baseId ? `${revisionId}..${baseId}` : revisionId

export const useRevisionDiffs = () => {
  const api = useEditorApi()
  const states = ref<Record<string, DiffState>>({})

  const load = async (revisionId: string, baseId?: string | null) => {
    const key = diffCacheKey(revisionId, baseId)
    const existing = states.value[key]
    if (existing && (existing.loading || existing.entries)) {
      return existing.entries
    }
    states.value = {
      ...states.value,
      [key]: { loading: true, error: '', entries: null }
    }
    try {
      const revision = await api<CatalogRevision>(
        `/v2/catalog/revisions/${revisionId}`,
        {
          query: { include: 'diff', ...(baseId ? { diff_base: baseId } : {}) }
        }
      )
      const entries = (revision.diff ?? []) as FieldDiffLike[]
      states.value = {
        ...states.value,
        [key]: { loading: false, error: '', entries }
      }
      return entries
    } catch (error) {
      states.value = {
        ...states.value,
        [key]: {
          loading: false,
          error: problemMessage(error, '无法读取改动'),
          entries: null
        }
      }
      return null
    }
  }

  const stateOf = (revisionId: string, baseId?: string | null) =>
    states.value[diffCacheKey(revisionId, baseId)]

  return { load, stateOf }
}
