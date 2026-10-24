import type {
  DisplayLanguage,
  EditorPreferences
} from '#shared/types/preferences'
import {
  DEFAULT_PREFERENCES,
  DISPLAY_LANGUAGE_OPTIONS,
  PREFERENCES_STORAGE_KEY
} from '~/constants/preferences'

const isDisplayLanguage = (value: unknown): value is DisplayLanguage =>
  DISPLAY_LANGUAGE_OPTIONS.some((option) => option.value === value)

export const readPreferences = (): EditorPreferences => {
  try {
    const raw = JSON.parse(
      localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}'
    ) as Partial<EditorPreferences>
    return {
      displayLanguage: isDisplayLanguage(raw.displayLanguage)
        ? raw.displayLanguage
        : DEFAULT_PREFERENCES.displayLanguage
    }
  } catch {
    return { ...DEFAULT_PREFERENCES }
  }
}

export const writePreferences = (preferences: EditorPreferences) => {
  try {
    localStorage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify(preferences))
  } catch {
    return
  }
}
