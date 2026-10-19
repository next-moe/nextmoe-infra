import type { CatalogVocabulary, EditVocabulary } from '#shared/types/edit'

export const useVocabularies = () => {
  const api = useEditorApi()
  const byName = useState<Record<string, EditVocabulary>>(
    'edit-vocabularies',
    () => ({})
  )

  const load = async (names: string[]) => {
    const missing = names.filter((name) => !byName.value[name])
    if (missing.length === 0) {
      return
    }
    const loaded = await Promise.all(
      missing.map((name) =>
        api<CatalogVocabulary>(`/v2/vocabularies/${encodeURIComponent(name)}`)
          .then((vocab) => [
            name,
            {
              name,
              values: (vocab.values ?? []).map((entry) => ({
                value: entry.value,
                display_name: entry.display_name,
                description: entry.description
              }))
            }
          ])
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
