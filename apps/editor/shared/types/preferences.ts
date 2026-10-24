export type DisplayLanguage = 'zh' | 'ja' | 'en'

export interface EditorPreferences {
  displayLanguage: DisplayLanguage
}

export interface NameSource {
  display_name?: string | null
  lang?: string | null
  olang?: string | null
  localized?: Record<
    string,
    { value?: string; is_machine?: boolean } | undefined
  > | null
  name_zh?: string | null
}
