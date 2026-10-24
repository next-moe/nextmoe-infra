import type { RefNameItem } from '~/utils/refNameBatcher'

const COLLECTION_OF: Record<string, string> = {
  work: 'works',
  company: 'companies',
  character: 'characters',
  release: 'releases',
  tag: 'tags',
  engine: 'engines',
  series: 'series',
  person: 'persons',
  credit_name: 'credit-names',
  trait: 'traits'
}

export interface EntityRef {
  target_object: string
  entity_id: string | number
}

export const entityKey = (ref: EntityRef) =>
  `${ref.target_object}:${ref.entity_id}`

export const useEntityNames = () => {
  const batcher = useRefNameBatcher()
  const preferences = useEditorPreferences()
  const names = ref<Record<string, RefNameItem>>({})

  const resolve = async (refs: EntityRef[]) => {
    const keys = [
      ...new Set(
        refs
          .filter((ref) => COLLECTION_OF[ref.target_object])
          .map(entityKey)
          .filter((key) => !names.value[key])
      )
    ]
    if (keys.length === 0) {
      return
    }
    const found = await batcher.request(keys)
    names.value = {
      ...names.value,
      ...Object.fromEntries(
        [...found].filter(([, item]) => item).map(([key, item]) => [key, item!])
      )
    }
  }

  const nameOf = (ref: EntityRef) =>
    preferences.nameOf(names.value[entityKey(ref)]) || `#${ref.entity_id}`

  return { resolve, nameOf }
}
