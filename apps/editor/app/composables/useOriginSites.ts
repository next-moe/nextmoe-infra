import { ORIGIN_SITE_LABELS } from '~/constants/catalog'

export const useOriginSites = () => {
  const { byName, load } = useVocabularies()
  onMounted(() => load(['origin_site']))

  const entryOf = (origin: string | null | undefined) =>
    origin
      ? byName.value.origin_site?.values.find((entry) => entry.value === origin)
      : undefined

  const originLabel = (origin: string | null | undefined) =>
    origin ? (ORIGIN_SITE_LABELS[origin] ?? origin) : ''

  const profileUrl = (
    origin: string | null | undefined,
    uid: string | number | null | undefined
  ) => {
    const template = entryOf(origin)?.profile_url
    if (!template || uid === null || uid === undefined || uid === '') {
      return null
    }
    return template.replace('{id}', encodeURIComponent(String(uid)))
  }

  return { originLabel, profileUrl, entryOf }
}
