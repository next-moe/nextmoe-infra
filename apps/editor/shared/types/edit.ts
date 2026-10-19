import type { components } from './generated/catalog-v2-api'

type Schemas = components['schemas']

export type CatalogSnapshot = Schemas['SnapshotRecord']
export type CatalogEditImage = Schemas['EditImage']
export type CatalogVocabulary = Schemas['Vocabulary']
export type CatalogSchemaMember = Schemas['SchemaElementMember']

export type EditRefFamily =
  | 'work'
  | 'tag'
  | 'company'
  | 'engine'
  | 'series'
  | 'character'
  | 'credit_name'
  | 'person'
  | 'role'
  | 'trait'
  | 'release'

export type EditControl =
  | 'text'
  | 'textarea'
  | 'vocab'
  | 'switch'
  | 'int'
  | 'ref'
  | 'refList'
  | 'stringList'
  | 'objectList'
  | 'titles'
  | 'intros'
  | 'images'
  | 'regrade'
  | 'roster'
  | 'readonly'

export interface EditMemberConfig {
  label?: string
  ref?: EditRefFamily
  placeholder?: string
}

export interface EditFieldConfig {
  label: string
  group: string
  control?: EditControl
  ref?: EditRefFamily
  members?: Record<string, EditMemberConfig>
  help?: string
  imagePreset?: 'cover' | 'screenshot'
  upstreamTitle?: string
}

export interface EditGroup {
  value: string
  label: string
  icon: string
}

export interface EditEntityConfig {
  family: string
  entityType: string
  groups: EditGroup[]
  fields: Record<string, EditFieldConfig>
}

export interface EditVocabularyValue {
  value: string
  display_name: string
  description?: string
}

export interface EditVocabulary {
  name: string
  values: EditVocabularyValue[]
}

export interface EditIssue {
  key: string
  index?: number
  member?: string
  message: string
}

export interface EditUpstreamRow {
  key: string
  match?: string
  identity: string | null
  characterId?: string
  label: string
  detail?: string
  text?: string
  lang?: string
  source?: string
  imageUrl?: string
  thumbhash?: string | null
  ref?: { family: EditRefFamily; id: string }
}

export type EditFieldValues = Record<string, unknown>

export interface EditStoredDraft {
  savedAt: string
  baseFingerprint: string
  values: EditFieldValues
}
