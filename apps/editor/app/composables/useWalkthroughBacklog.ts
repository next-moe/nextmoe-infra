import type { ActivityList } from '#shared/types/activity'
import type {
  CatalogProposal,
  CatalogWalkthrough,
  CatalogWalkthroughImportReview
} from '#shared/types/catalog'
import { WT_FIELD } from '~/constants/walkthrough'

export interface WalkthroughBacklogEntry {
  review: CatalogWalkthroughImportReview
  sourceUrl: string | null
  pendingPublishId: string | null
}

export const useWalkthroughBacklog = (entityId: string) => {
  const api = useEditorApi()

  const findPendingPublish = async () => {
    const open = await api<ActivityList<CatalogProposal>>('/v2/me/proposals', {
      query: {
        state: 'open',
        object: 'walkthrough',
        entity_id: entityId,
        field: WT_FIELD.hidden,
        include: 'patch',
        limit: 10
      }
    }).catch(() => null)
    const proposal = open?.items?.find((item) => {
      const patch = (item.effective_patch ?? item.patch) as
        Record<string, unknown> | undefined
      return patch?.[WT_FIELD.hidden] === false
    })
    return proposal ? proposal.id : null
  }

  const { data, refresh } = useLazyAsyncData(
    `walkthrough-backlog-${entityId}`,
    async (): Promise<WalkthroughBacklogEntry | null> => {
      const page = await api<{ items: CatalogWalkthrough[] }>(
        '/v2/me/walkthrough-backlog',
        { query: { ids: entityId } }
      ).catch(() => null)
      const item = page?.items[0]
      if (!item?.import_review) return null
      return {
        review: item.import_review,
        sourceUrl: item.source_url || null,
        pendingPublishId: await findPendingPublish()
      }
    }
  )

  return { entry: data, refresh }
}
