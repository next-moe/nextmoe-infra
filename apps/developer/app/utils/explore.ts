import { vocabularyValuesZh } from '~/generated/vocabularies-zh'
import type {
  ExploreImage,
  ExploreLocalized,
  ExploreWorkBrief
} from '~~/shared/types/explore'

export const vocabLabel = (vocab: string, key?: string | null): string =>
  key ? (vocabularyValuesZh[vocab]?.[key]?.displayName ?? key) : ''

export const isImageShown = (img: ExploreImage, nsfw: boolean): boolean =>
  nsfw ||
  ((img.sexual ?? 'safe') === 'safe' && (img.violence ?? 'tame') === 'tame')

// /v2/catalog/works/{id} serves galgame only, but relations, appearances and
// name credits also list anime and manga works: every one of those links on
// /explore/work/4 opened "work not found".
export const isBrowsableWork = (work?: ExploreWorkBrief | null): boolean =>
  work?.medium === 'galgame'

export const exploreWorkPath = (id: string, nsfw: boolean): string =>
  `/explore/work/${id}${nsfw ? '?nsfw=1' : ''}`

const ZH_LOCALES = ['zh-Hans', 'zh', 'zh-Hant']

export const zhLocalized = (
  localized?: Record<string, ExploreLocalized>
): ExploreLocalized | null => {
  for (const locale of ZH_LOCALES) {
    const row = localized?.[locale]
    if (row?.value) return row
  }
  return null
}

export const formatPlaytime = (minutes: number): string =>
  minutes < 60 ? `${minutes} 分钟` : `${Math.round(minutes / 60)} 小时`
