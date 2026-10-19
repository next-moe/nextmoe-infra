import type { CatalogSchemaField } from '../types/catalog'
import type {
  CatalogSchemaMember,
  EditIssue,
  EditVocabulary
} from '../types/edit'
import { normalizeFieldValue, rowIdentity } from './editModel'

const isBlank = (value: unknown) =>
  value === null ||
  value === undefined ||
  (typeof value === 'string' && value.trim() === '')

const isRefId = (value: unknown) => /^[1-9]\d{0,18}$/.test(String(value ?? ''))

const DUPLICATE_MESSAGE: Record<string, string> = {
  titles: '同一语言下有重复的标题',
  intros: '同一语言只能有一段简介',
  labels: '同一厂商的同一身份重复了',
  covers: '同一张图片重复了',
  screenshots: '同一张图片重复了',
  credits: '同一职位下重复的署名',
  roster: '同一角色重复了'
}

const memberIssue = (
  field: CatalogSchemaField,
  index: number,
  member: CatalogSchemaMember,
  value: unknown,
  vocabs: Record<string, EditVocabulary>
): EditIssue | null => {
  const at = { key: field.key, index, member: member.key }
  if (isBlank(value)) {
    return member.nullable ? null : { ...at, message: '必填' }
  }
  if (member.type === 'ref' && !isRefId(value)) {
    return { ...at, message: '请选择一个条目' }
  }
  if (member.vocabulary && vocabs[member.vocabulary]) {
    const size = vocabs[member.vocabulary]!.values.length
    if (member.type === 'int') {
      const code = Number(value) - (member.base ?? 0)
      if (!Number.isInteger(code) || code < 0 || code >= size) {
        return { ...at, message: '取值不在可选范围内' }
      }
    }
  }
  return null
}

export const validateField = (
  field: CatalogSchemaField,
  raw: unknown,
  vocabs: Record<string, EditVocabulary> = {}
): EditIssue[] => {
  const issues: EditIssue[] = []
  const value = normalizeFieldValue(field, raw)

  if (field.field_type === 'text') {
    if (!field.nullable && isBlank(value)) {
      issues.push({ key: field.key, message: '不能为空' })
    }
    return issues
  }

  if (field.field_type !== 'list') {
    return issues
  }
  if (!Array.isArray(value)) {
    issues.push({ key: field.key, message: '应为列表' })
    return issues
  }

  const cap = field.key.endsWith('.suppressed')
    ? field.max_suppressed || field.max_elements
    : field.max_elements
  if (cap > 0 && value.length > cap) {
    issues.push({
      key: field.key,
      message: `最多 ${cap} 项，当前 ${value.length} 项`
    })
  }

  const element = field.element
  const seen = new Map<string, number>()
  value.forEach((row, index) => {
    if (element?.type === 'ref' && !isRefId(row)) {
      issues.push({ key: field.key, index, message: '请选择一个条目' })
    }
    if (element?.type === 'object') {
      for (const member of element.members ?? []) {
        const issue = memberIssue(
          field,
          index,
          member,
          (row as Record<string, unknown> | null)?.[member.key],
          vocabs
        )
        if (issue) {
          issues.push(issue)
        }
      }
    }
    const identity = rowIdentity(field.key, row)
    if (seen.has(identity)) {
      const bare = field.key.split('.').slice(2).join('.')
      issues.push({
        key: field.key,
        index,
        message: DUPLICATE_MESSAGE[bare] ?? '与前面的项重复'
      })
    } else {
      seen.set(identity, index)
    }
  })
  return issues
}

export const validateFields = (
  fields: CatalogSchemaField[],
  values: Record<string, unknown>,
  keys: string[],
  vocabs: Record<string, EditVocabulary> = {}
) => {
  const byKey = new Map(fields.map((field) => [field.key, field]))
  return keys.flatMap((key) => {
    const field = byKey.get(key)
    return field ? validateField(field, values[key], vocabs) : []
  })
}
