import type { CatalogWork } from '#shared/types/catalog'
import type { EditUpstreamRow } from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'

export interface WorkUpstream {
  rows: Record<string, EditUpstreamRow[]>
  images: Record<string, { url: string; thumbhash: string | null }>
  rosterIdentity: Record<string, string>
}

const HUMAN_SOURCES = new Set(['curated', 'user'])

const isUpstream = (source: string | null | undefined) =>
  !HUMAN_SOURCES.has(source ?? '')

const withIdentity = (value: unknown) =>
  typeof value === 'string' && value ? value : null

const excerpt = (text: string, size = 80) =>
  text.length > size ? `${text.slice(0, size)}…` : text

export const workUpstream = (
  work: CatalogWork,
  roleIdByKey: Map<string, string>
): WorkUpstream => {
  const rows: Record<string, EditUpstreamRow[]> = {}
  const images: WorkUpstream['images'] = {}

  const tags = new Map<string, EditUpstreamRow & { sources: string[] }>()
  for (const tag of work.tags ?? []) {
    if (!tag.id || !isUpstream(tag.source)) {
      continue
    }
    const existing = tags.get(tag.id)
    if (existing) {
      existing.sources.push(tag.source)
      existing.source = existing.sources.join('、')
      continue
    }
    tags.set(tag.id, {
      key: `tag:${tag.id}`,
      match: tag.id,
      identity: `tag:${tag.id}`,
      label: tag.display_name,
      detail:
        tag.spoiler && tag.spoiler !== 'none'
          ? vocabLabel('spoiler', tag.spoiler)
          : undefined,
      source: tag.source,
      sources: [tag.source],
      ref: { family: 'tag', id: tag.id }
    })
  }
  rows['catalog.work.tag_ids'] = [...tags.values()]

  rows['catalog.work.labels'] = (work.companies ?? []).map((company) => ({
    key: `${company.id}:${company.attribution_role}`,
    identity: withIdentity((company as { identity?: unknown }).identity),
    label: company.display_name,
    detail: vocabLabel('attribution_role', company.attribution_role),
    ref: { family: 'company', id: company.id }
  }))

  rows['catalog.work.engine_ids'] = (work.engines ?? []).map((engine) => ({
    key: engine.id,
    match: engine.id,
    identity: withIdentity((engine as { identity?: unknown }).identity),
    label: engine.display_name,
    ref: { family: 'engine', id: engine.id }
  }))

  rows['catalog.work.series_ids'] = (work.series ?? [])
    .filter((series) => isUpstream(series.source))
    .map((series) => ({
      key: series.id,
      match: series.id,
      identity: withIdentity((series as { identity?: unknown }).identity),
      label: series.display_name,
      detail: `${series.member_count} 部作品`,
      source: series.source,
      ref: { family: 'series', id: series.id }
    }))

  rows['catalog.work.links'] = (work.links ?? []).map((link) => ({
    key: link.url,
    match: link.url,
    identity: withIdentity((link as { identity?: unknown }).identity),
    label: link.url,
    detail: link.is_identity ? '身份链接' : undefined,
    source: link.source
  }))

  rows['catalog.work.intros'] = (work.intros ?? [])
    .filter((intro) => isUpstream(intro.source))
    .map((intro) => ({
      key: `${intro.source}:${intro.lang}`,
      identity: withIdentity((intro as { identity?: unknown }).identity),
      label: `${vocabLabel('intro_lang', intro.lang)}${intro.is_machine ? '（机翻）' : ''}`,
      detail: excerpt(intro.value ?? ''),
      text: intro.value ?? '',
      lang: intro.lang,
      source: intro.source
    }))

  for (const kind of ['covers', 'screenshots'] as const) {
    const list = (work[kind] ?? []) as {
      hash: string
      url: string
      thumbhash?: string | null
      source: string
      identity?: unknown
      cover_kind?: string
      sexual?: string | null
      caption?: string
    }[]
    for (const image of list) {
      images[image.hash] = {
        url: image.url,
        thumbhash: image.thumbhash ?? null
      }
    }
    rows[`catalog.work.${kind}`] = list
      .filter((image) => isUpstream(image.source))
      .map((image) => ({
        key: image.hash,
        match: image.hash,
        identity: withIdentity(image.identity),
        label: image.cover_kind || image.caption || '',
        detail: image.sexual ? vocabLabel('sexual', image.sexual) : undefined,
        source: image.source,
        imageUrl: image.url,
        thumbhash: image.thumbhash ?? null
      }))
  }

  const credits: EditUpstreamRow[] = []
  for (const group of work.credits ?? []) {
    const roleId = roleIdByKey.get(group.role_key) ?? ''
    for (const entry of group.credits ?? []) {
      credits.push({
        key:
          entry.identity ||
          `${group.role_key}:${entry.id}:${entry.character_id ?? 0}`,
        match: `${roleId}:${entry.id}:${entry.character_id ?? 0}`,
        identity: withIdentity(entry.identity),
        label: entry.display_name,
        detail: group.role_name,
        ref: { family: 'credit_name', id: entry.id },
        characterId: entry.character_id ?? undefined
      })
    }
  }
  rows['catalog.work.credits'] = credits

  const rosterIdentity: Record<string, string> = {}
  for (const character of work.characters ?? []) {
    if (character.identity) {
      rosterIdentity[character.id] = character.identity
    }
  }

  return { rows, images, rosterIdentity }
}
