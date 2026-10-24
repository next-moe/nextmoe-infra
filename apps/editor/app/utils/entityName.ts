import type { DisplayLanguage, NameSource } from '#shared/types/preferences'
import { LOCALIZED_KEYS } from '~/constants/preferences'

const ownLanguageOf = (source: NameSource) =>
  (source.lang || source.olang || '').toLowerCase()

const isLanguage = (tag: string, lang: DisplayLanguage) =>
  tag === lang || tag.startsWith(`${lang}-`)

export const localizedName = (
  localized: NameSource['localized'],
  lang: DisplayLanguage
) => {
  const found = LOCALIZED_KEYS[lang]
    .map((key) => localized?.[key])
    .filter((entry) => entry?.value)
  return (found.find((entry) => !entry?.is_machine) ?? found[0])?.value ?? ''
}

export const pickName = (source: NameSource, lang: DisplayLanguage) => {
  const display = source.display_name ?? ''
  const own = ownLanguageOf(source)
  if (own && isLanguage(own, lang)) {
    return display || localizedName(source.localized, lang)
  }
  const translated =
    lang === 'ja' && !own
      ? ''
      : localizedName(source.localized, lang) ||
        (lang === 'zh' ? (source.name_zh ?? '') : '')
  return translated || display || source.name_zh || ''
}

export const alternateName = (source: NameSource, lang: DisplayLanguage) => {
  const display = source.display_name ?? ''
  return display && display !== pickName(source, lang) ? display : ''
}
