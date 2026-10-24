import type {
  DisplayLanguage,
  EditorPreferences,
  NameSource
} from '#shared/types/preferences'

export const useEditorPreferences = () => {
  const preferences = useState<EditorPreferences>(
    'editor-preferences',
    readPreferences
  )

  const displayLanguage = computed(() => preferences.value.displayLanguage)

  const setDisplayLanguage = (displayLanguage: DisplayLanguage) => {
    preferences.value = { ...preferences.value, displayLanguage }
    writePreferences(preferences.value)
  }

  const nameOf = (source: NameSource | null | undefined) =>
    source ? pickName(source, displayLanguage.value) : ''

  const otherNameOf = (source: NameSource | null | undefined) =>
    source ? alternateName(source, displayLanguage.value) : ''

  return {
    preferences,
    displayLanguage,
    setDisplayLanguage,
    nameOf,
    otherNameOf
  }
}
