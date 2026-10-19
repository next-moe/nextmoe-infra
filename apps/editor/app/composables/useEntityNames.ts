const COLLECTION_OF: Record<string, string> = {
  work: 'works',
  company: 'companies',
  character: 'characters',
  release: 'releases',
  tag: 'tags',
  engine: 'engines',
  series: 'series'
}

export interface EntityRef {
  target_object: string
  entity_id: string | number
}

export const entityKey = (ref: EntityRef) =>
  `${ref.target_object}:${ref.entity_id}`

export const useEntityNames = () => {
  const api = useEditorApi()
  const names = ref<Record<string, string>>({})

  const resolve = async (refs: EntityRef[]) => {
    const byFamily = new Map<string, Set<string>>()
    for (const ref of refs) {
      const key = entityKey(ref)
      if (names.value[key] || !COLLECTION_OF[ref.target_object]) {
        continue
      }
      const ids = byFamily.get(ref.target_object) ?? new Set<string>()
      ids.add(String(ref.entity_id))
      byFamily.set(ref.target_object, ids)
    }
    if (byFamily.size === 0) {
      return
    }
    const found = await Promise.all(
      [...byFamily].map(async ([family, ids]) => {
        const list = await api<{
          items?: { id: string; display_name?: string }[]
        }>(`/v2/catalog/${COLLECTION_OF[family]}`, {
          query: { ids: [...ids].slice(0, 100).join(','), nsfw: true }
        }).catch(() => null)
        return (list?.items ?? []).map((item) => [
          `${family}:${item.id}`,
          item.display_name ?? ''
        ])
      })
    )
    names.value = { ...names.value, ...Object.fromEntries(found.flat()) }
  }

  const nameOf = (ref: EntityRef) =>
    names.value[entityKey(ref)] || `#${ref.entity_id}`

  return { resolve, nameOf }
}
