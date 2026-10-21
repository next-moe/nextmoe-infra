import type {
  CatalogSplitPreview,
  CatalogSplitPreviewRelease,
  CatalogSplitRequest,
  CatalogSplitTitle
} from '#shared/types/catalog'

export type SplitGroupBy = 'kind' | 'platform' | 'lang' | 'none'

export interface SplitDraft {
  displayName: string
  olang: string
  titles: CatalogSplitTitle[]
}

export interface SplitSelection {
  releases: string[]
  promote: string[]
}

export const refKey = (ref: { source: string; external_id: string }) =>
  `${ref.source}:${ref.external_id}`

export const parseRefKey = (key: string) => {
  const at = key.indexOf(':')
  return { source: key.slice(0, at), external_id: key.slice(at + 1) }
}

export const releaseMatches = (
  release: CatalogSplitPreviewRelease,
  query: string
) => {
  const q = query.trim().toLowerCase()
  if (!q) {
    return true
  }
  return [
    release.title,
    release.platform,
    release.lang,
    release.vndb_rid ?? '',
    release.id,
    release.release_date ?? ''
  ].some((field) => field.toLowerCase().includes(q))
}

export interface ReleaseGroup {
  key: string
  releases: CatalogSplitPreviewRelease[]
}

export const groupReleases = (
  releases: CatalogSplitPreviewRelease[],
  by: SplitGroupBy
): ReleaseGroup[] => {
  if (by === 'none') {
    return [{ key: '', releases }]
  }
  const groups = new Map<string, CatalogSplitPreviewRelease[]>()
  for (const release of releases) {
    const key =
      by === 'kind'
        ? release.release_kind
        : by === 'platform'
          ? release.platform
          : release.lang
    groups.set(key, [...(groups.get(key) ?? []), release])
  }
  return [...groups.entries()].map(([key, list]) => ({ key, releases: list }))
}

// A shift-click selects every visible release between the anchor and the
// clicked one, to the anchor's new state.
export const rangeSelect = (
  visible: string[],
  selected: string[],
  anchor: string | null,
  clicked: string,
  shift: boolean
): string[] => {
  const has = selected.includes(clicked)
  if (!shift || !anchor || !visible.includes(anchor)) {
    return has
      ? selected.filter((id) => id !== clicked)
      : [...selected, clicked]
  }
  const a = visible.indexOf(anchor)
  const b = visible.indexOf(clicked)
  const span = visible.slice(Math.min(a, b), Math.max(a, b) + 1)
  const on = selected.includes(anchor)
  const rest = selected.filter((id) => !span.includes(id))
  return on ? [...rest, ...span] : rest
}

export const previewQuery = (workId: string, selection: SplitSelection) => ({
  object: 'work',
  target_id: workId,
  ...(selection.releases.length
    ? { release_ids: [...selection.releases].sort(byNumber).join(',') }
    : {}),
  ...(selection.promote.length ? { promote: selection.promote.join(',') } : {})
})

const byNumber = (a: string, b: string) => Number(a) - Number(b)

export const draftFromPreview = (preview: CatalogSplitPreview): SplitDraft => ({
  displayName: preview.suggested_name,
  olang: preview.suggested_lang || preview.olang,
  titles: preview.titles.map((t) => ({ ...t }))
})

export const draftFromRequest = (request: CatalogSplitRequest): SplitDraft => ({
  displayName: request.display_name,
  olang: request.olang,
  titles: request.titles.map((t) => ({ ...t }))
})

export const cleanTitles = (titles: CatalogSplitTitle[]) =>
  titles
    .map((t) => ({ ...t, lang: t.lang.trim(), title: t.title.trim() }))
    .filter((t) => t.title)

export const draftBody = (selection: SplitSelection, draft: SplitDraft) => ({
  release_ids: [...selection.releases].sort(byNumber),
  display_name: draft.displayName.trim(),
  olang: draft.olang.trim(),
  titles: cleanTitles(draft.titles),
  promote_refs: selection.promote.map(parseRefKey)
})

export const sameDraft = (
  a: { selection: SplitSelection; draft: SplitDraft },
  b: { selection: SplitSelection; draft: SplitDraft }
) =>
  JSON.stringify(draftBody(a.selection, a.draft)) ===
  JSON.stringify(draftBody(b.selection, b.draft))

export const draftProblems = (
  selection: SplitSelection,
  draft: SplitDraft
): string[] => {
  const out: string[] = []
  if (!selection.releases.length) {
    out.push('选择要拆出去的发行')
  }
  if (!draft.displayName.trim()) {
    out.push('填写新作品的显示名')
  }
  if (!/^[a-z]{2,3}(-[A-Za-z]{2,4})?$/.test(draft.olang.trim())) {
    out.push('选择新作品的原语言')
  }
  return out
}

export const countOf = (
  counts: { key: string; count: number }[] | undefined,
  key: string
) => counts?.find((c) => c.key === key)?.count ?? 0
