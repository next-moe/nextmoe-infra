export { imageUrl } from '../imageCdn'

export const MEMBER_LABELS: Record<string, string> = {
  lang: '语言',
  title: '标题',
  kind: '类型',
  latin: '罗马字',
  intro: '简介',
  label_id: '厂商',
  role_id: '职位',
  credit_name_id: '署名',
  character_id: '角色',
  note: '备注',
  spoiler: '剧透',
  image_hash: '图片',
  portrait_pinned: '竖图',
  sexual: '色情',
  violence: '暴力',
  caption: '说明',
  trait_id: '特质',
  lie: '谎言',
  relation: '关系',
  name: '名称',
  alias: '别名'
}

export type RefCollection =
  | 'tags'
  | 'companies'
  | 'engines'
  | 'series'
  | 'characters'
  | 'credit-names'
  | 'roles'
  | 'persons'
  | 'traits'
  | 'releases'
  | 'works'

const FIELD_REF: Record<string, RefCollection> = {
  tag_ids: 'tags',
  engine_ids: 'engines',
  engine_id: 'engines',
  series_ids: 'series',
  instance_of: 'characters',
  person_id: 'persons',
  primary_credit_name_id: 'credit-names'
}

const MEMBER_REF: Record<string, RefCollection> = {
  label_id: 'companies',
  role_id: 'roles',
  credit_name_id: 'credit-names',
  character_id: 'characters',
  trait_id: 'traits'
}

export const refCollectionOf = (fieldKey: string, member?: string) => {
  if (member) {
    return MEMBER_REF[member] ?? null
  }
  const bare =
    fieldKey
      .replace(/\.suppressed$/, '')
      .split('.')
      .pop() ?? ''
  return FIELD_REF[bare] ?? null
}

export interface VocabularyValue {
  value: string
  display_name?: string
}

export interface VocabularyInfo {
  values: VocabularyValue[]
  labels?: Record<string, string>
}

export const vocabularyLabel = (
  raw: unknown,
  vocabulary: VocabularyInfo | undefined,
  encoding: 'token' | 'int' | undefined,
  base = 0
) => {
  if (raw === null || raw === undefined || raw === '') {
    return '—'
  }
  if (!vocabulary) {
    return String(raw)
  }
  const token =
    encoding === 'int' && typeof raw === 'number'
      ? vocabulary.values[raw - base]?.value
      : String(raw)
  if (token === undefined) {
    return String(raw)
  }
  return (
    vocabulary.labels?.[token] ||
    vocabulary.values.find((v) => v.value === token)?.display_name ||
    token
  )
}

export const scalarText = (value: unknown) => {
  if (value === null || value === undefined || value === '') {
    return '—'
  }
  if (typeof value === 'boolean') {
    return value ? '是' : '否'
  }
  if (typeof value === 'object') {
    return JSON.stringify(value)
  }
  return String(value)
}
