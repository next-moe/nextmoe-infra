import type { CatalogVocabulary, EditVocabulary } from '#shared/types/edit'

const CACHE_KEY = 'nextmoe-editor:vocabularies:v2'
const CACHE_MAX_AGE_MS = 6 * 60 * 60 * 1000

let catalogue: Promise<Record<string, EditVocabulary> | null> | null = null

const toEditVocabulary = (vocab: CatalogVocabulary): EditVocabulary => ({
  name: vocab.name,
  values: (vocab.values ?? []).map((entry) => ({
    value: entry.value,
    display_name: entry.display_name,
    description: entry.description,
    ...(entry.profile_url ? { profile_url: entry.profile_url } : {})
  }))
})

export const useVocabularies = () => {
  const api = useEditorApi()
  const byName = useState<Record<string, EditVocabulary>>(
    'edit-vocabularies',
    () => ({})
  )

  const loadCatalogue = () => {
    catalogue ??= (async () => {
      const cached = readLocalCache<Record<string, EditVocabulary>>(
        CACHE_KEY,
        CACHE_MAX_AGE_MS
      )
      if (cached) {
        return cached
      }
      const list = await api<{ items?: CatalogVocabulary[] }>(
        '/v2/vocabularies',
        { query: { limit: 100 } }
      ).catch(() => null)
      if (!list?.items?.length) {
        catalogue = null
        return null
      }
      const all = Object.fromEntries(
        list.items.map((vocab) => [vocab.name, toEditVocabulary(vocab)])
      )
      writeLocalCache(CACHE_KEY, all)
      return all
    })()
    return catalogue
  }

  const load = async (names: string[]) => {
    if (names.every((name) => byName.value[name])) {
      return
    }
    const all = await loadCatalogue()
    if (all) {
      byName.value = { ...all, ...byName.value }
    }
    const missing = names.filter((name) => !byName.value[name])
    if (missing.length === 0) {
      return
    }
    const loaded = await Promise.all(
      missing.map((name) =>
        api<CatalogVocabulary>(`/v2/vocabularies/${encodeURIComponent(name)}`)
          .then((vocab) => [name, toEditVocabulary(vocab)] as const)
          .catch(() => null)
      )
    )
    byName.value = {
      ...byName.value,
      ...Object.fromEntries(loaded.filter((entry) => entry !== null))
    }
  }

  return { byName, load }
}
