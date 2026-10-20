import type {
  CatalogColumnProvenance,
  CatalogEditContext,
  CatalogEditContextField,
  CatalogEditContextRow,
  CatalogSchemaField
} from '#shared/types/catalog'
import type {
  CatalogSchemaMember,
  EditFieldConfig,
  EditFieldValues,
  EditRefFamily,
  EditUpstreamRow
} from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'
import { imageUrl } from './imageCdn'

export interface ContextRowFormatter {
  memberText: (member: CatalogSchemaMember, value: unknown) => string
}

const TITLE_KINDS = ['official', 'alias', 'abbreviation', 'search']
const LINK_KINDS = ['身份链接', '待确认身份链接', '相关链接']
const ID_MEMBERS = [
  'tag_id',
  'series_id',
  'engine_id',
  'trait_id',
  'label_id',
  'id'
]

type Record_ = Record<string, unknown>

const asRecord = (value: unknown): Record_ =>
  value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record_)
    : {}

const str = (value: unknown) =>
  value === null || value === undefined ? '' : String(value)

const excerpt = (text: string, size = 80) =>
  text.length > size ? `${text.slice(0, size)}…` : text

const bareName = (fieldKey: string) => fieldKey.split('.').slice(2).join('.')

const isImageField = (field: CatalogSchemaField) =>
  (field.element?.members ?? []).some(
    (member) => member.key === 'image_hash' && member.type === 'imagehash'
  )

const pickMembers = (members: CatalogSchemaMember[], value: Record_) =>
  Object.fromEntries(
    members.filter((m) => m.key in value).map((m) => [m.key, value[m.key]])
  )

const refIdOf = (value: unknown) => {
  if (value === null || typeof value !== 'object') {
    return str(value)
  }
  const record = asRecord(value)
  const key = ID_MEMBERS.find((member) => record[member] != null)
  return key ? str(record[key]) : ''
}

const baseRow = (
  fieldKey: string,
  row: CatalogEditContextRow,
  index: number
): EditUpstreamRow => ({
  key: row.identity ?? `${fieldKey}:${row.source?.key ?? 'row'}:${index}`,
  identity: row.identity,
  label: '',
  source: row.source?.display,
  machine: row.machine_translated,
  suppressed: row.suppressed
})

const titleRow = (out: EditUpstreamRow, value: Record_): EditUpstreamRow => {
  const kind = TITLE_KINDS[Number(value.kind)] ?? ''
  return {
    ...out,
    label: str(value.title),
    detail: [
      value.lang ? vocabLabel('olang', str(value.lang)) : '',
      kind ? vocabLabel('title_kind', kind) : ''
    ]
      .filter(Boolean)
      .join(' · ')
  }
}

const introRow = (out: EditUpstreamRow, value: Record_): EditUpstreamRow => {
  const text = str(value.intro)
  return {
    ...out,
    label: vocabLabel('intro_lang', str(value.lang)),
    detail: excerpt(text),
    text,
    lang: str(value.lang)
  }
}

const linkRow = (out: EditUpstreamRow, value: Record_): EditUpstreamRow => ({
  ...out,
  label: str(value.external_id),
  match: str(value.external_id),
  detail: [
    LINK_KINDS[Number(value.link_kind)] ?? '',
    value.dead ? '已失效' : ''
  ]
    .filter(Boolean)
    .join(' · ')
})

const imageRow = (out: EditUpstreamRow, value: Record_): EditUpstreamRow => {
  const hash = str(value.image_hash)
  return {
    ...out,
    match: hash,
    label: str(value.kind ?? value.caption),
    detail: value.sexual != null ? `色情 ${str(value.sexual)}` : undefined,
    imageUrl: imageUrl(hash),
    thumbhash: null
  }
}

const objectRow = (
  out: EditUpstreamRow,
  value: Record_,
  members: CatalogSchemaMember[],
  config: EditFieldConfig | undefined,
  identityOf: (row: unknown) => string,
  fmt: ContextRowFormatter
): EditUpstreamRow => {
  const refMembers = members.filter(
    (member) =>
      member.type === 'ref' &&
      member.key !== 'character_id' &&
      config?.members?.[member.key]?.ref &&
      value[member.key] != null
  )
  const refMember =
    refMembers.find(
      (member) => config?.members?.[member.key]?.ref !== 'role'
    ) ?? refMembers[0]
  const family = refMember
    ? (config?.members?.[refMember.key]?.ref as EditRefFamily)
    : undefined
  const characterId =
    members.some((member) => member.key === 'character_id') &&
    value.character_id != null &&
    refMember?.key !== 'character_id'
      ? str(value.character_id)
      : undefined
  const rest = members
    .filter(
      (member) =>
        member.key !== refMember?.key &&
        member.key !== 'character_id' &&
        member.key in value
    )
    .map((member) => fmt.memberText(member, value[member.key]))
    .filter(Boolean)
  const label = refMember
    ? `#${str(value[refMember.key])}`
    : (rest.shift() ?? '')
  return {
    ...out,
    label,
    detail: rest.join(' · ') || undefined,
    match: identityOf(pickMembers(members, value)),
    ref:
      refMember && family
        ? { family, id: str(value[refMember.key]) }
        : undefined,
    characterId
  }
}

export const upstreamRowsOf = (
  fieldKey: string,
  field: CatalogSchemaField,
  context: CatalogEditContextField | undefined,
  config: EditFieldConfig | undefined,
  identityOf: (row: unknown) => string,
  fmt: ContextRowFormatter
): EditUpstreamRow[] => {
  const bare = bareName(fieldKey)
  const members = field.element?.members ?? []
  return (context?.rows ?? [])
    .map((row, index) => ({ row, index }))
    .filter(({ row }) => !row.human_lane && row.present)
    .map(({ row, index }) => {
      const out = baseRow(fieldKey, row, index)
      const value = row.value
      if (field.element?.type === 'ref') {
        const id = refIdOf(value)
        const name = str(asRecord(value).name)
        return {
          ...out,
          label: name || `#${id}`,
          match: id,
          ref: config?.ref && id ? { family: config.ref, id } : undefined
        }
      }
      if (field.element?.type === 'text') {
        return value && typeof value === 'object'
          ? linkRow(out, asRecord(value))
          : { ...out, label: str(value), match: str(value) }
      }
      const record = asRecord(value)
      if (bare === 'titles') {
        return titleRow(out, record)
      }
      if (bare === 'intros') {
        return introRow(out, record)
      }
      if (isImageField(field)) {
        return imageRow(out, record)
      }
      return objectRow(out, record, members, config, identityOf, fmt)
    })
}

export const contextFieldMap = (context: CatalogEditContext | null) =>
  new Map((context?.fields ?? []).map((field) => [field.key, field]))

export const contextRosterIdentity = (
  field: CatalogEditContextField | undefined
) => {
  const out: Record<string, string> = {}
  for (const row of field?.rows ?? []) {
    const characterId = asRecord(row.value).character_id
    if (row.identity && characterId != null) {
      out[str(characterId)] = row.identity
    }
  }
  return out
}

export const contextProvenance = (context: CatalogEditContext | null) => {
  const out: Record<string, CatalogColumnProvenance[]> = {}
  for (const field of context?.fields ?? []) {
    if (field.provenance?.length) {
      out[field.key] = field.provenance
    }
  }
  return out
}

const collectHashes = (value: unknown, into: Set<string>) => {
  if (Array.isArray(value)) {
    for (const item of value) {
      collectHashes(item, into)
    }
    return
  }
  const hash = asRecord(value).image_hash
  if (typeof hash === 'string' && hash) {
    into.add(hash)
  }
}

export const contextImages = (
  fields: CatalogSchemaField[],
  values: EditFieldValues,
  context: CatalogEditContext | null
) => {
  const hashes = new Set<string>()
  for (const field of fields) {
    const value = values[field.key]
    if (
      field.field_type === 'imagehash' &&
      typeof value === 'string' &&
      value
    ) {
      hashes.add(value)
    } else if (field.field_type === 'list') {
      collectHashes(value, hashes)
    }
  }
  for (const field of context?.fields ?? []) {
    for (const row of field.rows ?? []) {
      collectHashes(row.value, hashes)
    }
  }
  return Object.fromEntries(
    [...hashes].map((hash) => [hash, { url: imageUrl(hash), thumbhash: null }])
  )
}

export const latestWriter = (
  provenance: CatalogColumnProvenance[] | undefined
) => {
  let latest: { source: string; human: boolean; at: string } | null = null
  for (const column of provenance ?? []) {
    const head = column.history?.[0]
    if (head && (!latest || (head.at ?? '') > latest.at)) {
      latest = { source: head.source, human: head.human, at: head.at ?? '' }
    }
  }
  return latest
}
