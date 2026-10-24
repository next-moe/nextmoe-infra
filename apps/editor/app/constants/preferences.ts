import type {
  DisplayLanguage,
  EditorPreferences,
  NameSource
} from '#shared/types/preferences'

export const PREFERENCES_STORAGE_KEY = 'nextmoe-editor:preferences:v1'

export const DEFAULT_PREFERENCES: EditorPreferences = {
  displayLanguage: 'zh'
}

export const DISPLAY_LANGUAGE_OPTIONS: {
  value: DisplayLanguage
  label: string
  description: string
}[] = [
  {
    value: 'zh',
    label: '中文',
    description: '优先显示简体中文名，其次繁体中文名'
  },
  {
    value: 'ja',
    label: '日本語',
    description: '优先显示日文名，日文原作通常就是原名'
  },
  {
    value: 'en',
    label: 'English',
    description: '优先显示英文名'
  }
]

export const LOCALIZED_KEYS: Record<DisplayLanguage, string[]> = {
  zh: ['zh-Hans', 'zh-Hant', 'zh'],
  ja: ['ja'],
  en: ['en']
}

export const COLOR_MODE_OPTIONS = [
  { value: 'light', label: '浅色', icon: 'lucide:sun' },
  { value: 'dark', label: '深色', icon: 'lucide:moon' },
  { value: 'system', label: '跟随系统', icon: 'lucide:monitor' }
] as const

export const DISPLAY_LANGUAGE_SAMPLE: NameSource = {
  display_name: '枯れない世界と終わる花',
  olang: 'ja',
  localized: {
    en: { value: 'Karenai Sekai to Owaru Hana' },
    ja: { value: '枯れない世界と終わる花' },
    'zh-Hans': { value: '永不枯萎的世界与终结之花' }
  }
}
