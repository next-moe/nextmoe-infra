import type { CatalogWork } from '#shared/types/catalog'
import { WORK_DETAIL_INCLUDE } from '~/constants/catalog'

export const useWorkUpstream = (workId: MaybeRefOrGetter<string>) => {
  const api = useEditorApi()
  const refs = useRefNames()
  const work = ref<CatalogWork | null>(null)

  const upstream = computed(() =>
    work.value
      ? workUpstream(
          work.value,
          new Map(Object.entries(refs.roleIdByKey.value))
        )
      : null
  )

  const load = async () => {
    const [loaded] = await Promise.all([
      api<CatalogWork>(`/v2/catalog/works/${toValue(workId)}`, {
        query: { nsfw: true, view: 'full', include: WORK_DETAIL_INCLUDE }
      }).catch(() => null),
      refs.loadRoles()
    ])
    work.value = loaded
  }

  return { work, upstream, load }
}
