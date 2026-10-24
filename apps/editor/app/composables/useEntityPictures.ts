import type {
  CatalogEntityList,
  CatalogEntityPicture,
  CatalogEntitySummary
} from '#shared/types/catalog'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'
import {
  LIST_PICTURE_INCLUDE,
  PICTURE_KEYS,
  type PictureKey
} from '~/constants/entityPictures'

const BATCH = 100

const pictureIn = (item: CatalogEntitySummary, keys = PICTURE_KEYS) =>
  keys
    .map((key) => item[key] as CatalogEntityPicture | null | undefined)
    .find((picture) => picture?.url) ?? null

export const useEntityPictures = (family: EditFamily) => {
  const api = useEditorApi()
  const pictures = ref<Record<string, CatalogEntityPicture>>({})

  const fetchPictures = async (
    collection: string,
    ids: string[],
    key: PictureKey,
    include?: string
  ) => {
    const found = new Map<string, CatalogEntityPicture>()
    for (let i = 0; i < ids.length; i += BATCH) {
      const chunk = ids.slice(i, i + BATCH)
      const list = await api<CatalogEntityList>(`/v2/catalog/${collection}`, {
        query: {
          ids: chunk.join(','),
          nsfw: true,
          ...(include ? { include } : {})
        }
      }).catch(() => null)
      for (const item of list?.items ?? []) {
        const picture = pictureIn(item, [key])
        if (picture) {
          found.set(String(item.id), picture)
        }
      }
    }
    return found
  }

  const borrow = async (
    items: CatalogEntitySummary[],
    ownerOf: (item: CatalogEntitySummary) => unknown,
    collection: string,
    key: PictureKey,
    include?: string
  ) => {
    const owners = new Map(
      items
        .map((item) => [item.id, ownerOf(item)] as const)
        .filter(([, owner]) => owner != null && owner !== '')
        .map(([id, owner]) => [id, String(owner)])
    )
    const found = await fetchPictures(
      collection,
      [...new Set(owners.values())],
      key,
      include
    )
    return Object.fromEntries(
      [...owners]
        .filter(([, owner]) => found.has(owner))
        .map(([id, owner]) => [id, found.get(owner)!])
    )
  }

  const load = async (items: CatalogEntitySummary[]) => {
    const listed = LIST_PICTURE_INCLUDE[family]
    const pending = items.filter(
      (item) =>
        !pictureIn(item) &&
        !pictures.value[item.id] &&
        !(listed && listed.key in item)
    )
    if (!pending.length) {
      return
    }
    let found: Record<string, CatalogEntityPicture> = {}
    if (listed) {
      found = Object.fromEntries(
        await fetchPictures(
          ENTITY_META[family].collection,
          pending.map((item) => item.id),
          listed.key,
          listed.include
        )
      )
    } else if (family === 'person') {
      found = await borrow(
        pending,
        (item) => item.primary_credit_name_id,
        'credit-names',
        'photo',
        'photo'
      )
    } else if (family === 'release') {
      found = await borrow(pending, (item) => item.work_id, 'works', 'cover')
    }
    pictures.value = { ...pictures.value, ...found }
  }

  const pictureOf = (item: CatalogEntitySummary) =>
    pictureIn(item) ?? pictures.value[item.id] ?? null

  return { load, pictureOf }
}
