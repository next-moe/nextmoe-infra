import type {
  CatalogMergePreview,
  CatalogMergePreviewAnchor,
  CatalogMergePreviewCount,
  CatalogMergePreviewField,
  CatalogMergePreviewRelease,
  CatalogMergePreviewTitle
} from '#shared/types/catalog'
import { WRITER_LABELS } from '~/constants/editEntities'
import { LINK_SOURCE_LABELS } from '~/utils/externalLinks'

export type MergeSide = 'source' | 'target'

export interface MergeChoices {
  fields: Record<string, MergeSide>
  hide: string[]
}

export interface StoredChoices {
  field_resolution?: Record<string, string> | null
  hide_release_ids?: string[] | null
}

const isEmpty = (value: unknown) =>
  value === null || value === undefined || value === ''

// Survivorship fills an empty target from the source whatever is chosen, and
// a person's value on the target is never replaced by a machine value: in
// either case only one side can actually survive.
export const fixedSide = (
  field: CatalogMergePreviewField
): MergeSide | null => {
  if (isEmpty(field.target_value)) {
    return 'source'
  }
  if (field.human_protected || isEmpty(field.source_value)) {
    return 'target'
  }
  return null
}

export const initialChoices = (
  preview: CatalogMergePreview,
  stored?: StoredChoices | null
): MergeChoices => {
  const fields: Record<string, MergeSide> = {}
  for (const field of preview.fields) {
    const pinned = fixedSide(field)
    const chosen = stored?.field_resolution?.[field.key]
    fields[field.key] =
      pinned ?? (chosen === 'source' ? 'source' : (field.default as MergeSide))
  }
  const hide = stored
    ? [...(stored.hide_release_ids ?? [])]
    : preview.releases
        .filter((release) => release.side === 'source' && release.duplicate_of)
        .map((release) => release.id)
  return { fields, hide }
}

export const choicesBody = (
  preview: CatalogMergePreview,
  choices: MergeChoices
) => {
  const fieldResolution: Record<string, MergeSide> = {}
  for (const field of preview.fields) {
    if (choices.fields[field.key] === 'source' && fixedSide(field) === null) {
      fieldResolution[field.key] = 'source'
    }
  }
  return {
    field_resolution: fieldResolution,
    hide_release_ids: [...choices.hide].sort((a, b) => Number(a) - Number(b))
  }
}

export const sameChoices = (
  preview: CatalogMergePreview,
  a: MergeChoices,
  b: MergeChoices
) =>
  JSON.stringify(choicesBody(preview, a)) ===
  JSON.stringify(choicesBody(preview, b))

export const chosenValue = (
  field: CatalogMergePreviewField,
  choices: MergeChoices
) =>
  choices.fields[field.key] === 'source'
    ? field.source_value
    : field.target_value

export const mergedTitles = (
  titles: CatalogMergePreviewTitle[]
): CatalogMergePreviewTitle[] => [
  ...titles.filter((title) => title.side === 'target'),
  ...titles
    .filter(
      (title) =>
        title.side === 'source' &&
        (title.fate === 'moves' || title.fate === 'becomes_alias')
    )
    .map((title) =>
      title.fate === 'becomes_alias'
        ? { ...title, lang: '', title_kind: 'alias' as const }
        : title
    )
]

export interface ResultRelease extends CatalogMergePreviewRelease {
  hidden: boolean
}

export const mergedReleases = (
  releases: CatalogMergePreviewRelease[],
  hide: string[]
): ResultRelease[] =>
  [...releases]
    .map((release) => ({
      ...release,
      hidden: release.side === 'source' && hide.includes(release.id)
    }))
    .sort((a, b) =>
      (a.release_date ?? '9999').localeCompare(b.release_date ?? '9999')
    )

export const mergedAnchors = (anchors: CatalogMergePreviewAnchor[]) =>
  anchors.filter((anchor) => anchor.fate !== 'duplicate')

export const countAfter = (count: CatalogMergePreviewCount) =>
  count.on_source + count.on_target - count.already_on_target

export const duplicateReleases = (preview: CatalogMergePreview) =>
  preview.releases.filter(
    (release) => release.side === 'source' && release.duplicate_of
  )

export const demotedAnchors = (preview: CatalogMergePreview) =>
  preview.anchors.filter(
    (anchor) =>
      anchor.side === 'source' &&
      (anchor.fate === 'demoted_related' || anchor.fate === 'demoted_probable')
  )

export const releaseDateText = (release: CatalogMergePreviewRelease) => {
  if (!release.release_date) {
    return '日期不明'
  }
  const [y, m] = release.release_date.split('-')
  if (release.release_date_precision === 'year') {
    return y!
  }
  if (release.release_date_precision === 'month') {
    return `${y}-${m}`
  }
  return release.release_date
}

export const swapQuery = (source: string | null, target: string | null) => ({
  source: target ?? undefined,
  target: source ?? undefined
})

export const sourceLabel = (key: string) =>
  LINK_SOURCE_LABELS[key] ?? WRITER_LABELS[key] ?? key.toUpperCase()
