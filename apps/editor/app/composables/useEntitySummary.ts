import type { CatalogEntitySummary } from '#shared/types/catalog'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

export const useEntitySummary = (
  family: EditFamily,
  entityId: MaybeRefOrGetter<string>
) => {
  const api = useEditorApi()
  return useAsyncData(
    () => `entity-summary-${family}-${toValue(entityId)}`,
    () =>
      api<CatalogEntitySummary>(
        `/v2/catalog/${ENTITY_META[family].collection}/${toValue(entityId)}`,
        { query: { nsfw: true } }
      ).catch(() => null),
    { watch: [() => toValue(entityId)] }
  )
}
