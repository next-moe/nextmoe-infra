import type { CatalogSchemaField } from '#shared/types/catalog'
import type { CatalogSchemaMember, EditRefFamily } from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'

export const useRowLabel = () => {
  const { editor, config, refs } = useEditorContext()
  const { byName } = useVocabularies()

  const vocabText = (
    slot: ReturnType<typeof memberVocabSlot>,
    wire: unknown
  ) => {
    if (!slot) {
      return String(wire ?? '')
    }
    const vocab = byName.value[slot.vocabulary]
    const token = vocabTokenOf(slot, vocab, wire)
    if (!token) {
      return '未设置'
    }
    const entry = vocab?.values.find((v) => v.value === token)
    return vocabLabel(slot.vocabulary, token, entry?.display_name)
  }

  const memberText = (
    fieldKey: string,
    member: CatalogSchemaMember,
    value: unknown
  ) => {
    if (value === null || value === undefined || value === '') {
      return ''
    }
    const family = config.fields[fieldKey]?.members?.[member.key]?.ref as
      EditRefFamily | undefined
    if (member.type === 'ref') {
      return family ? refs.nameOf(family, value) || `#${value}` : `#${value}`
    }
    if (member.vocabulary) {
      return vocabText(memberVocabSlot(member), value)
    }
    if (member.type === 'bool') {
      return value ? '是' : ''
    }
    if (member.type === 'imagehash') {
      return `${String(value).slice(0, 10)}…`
    }
    return String(value)
  }

  const scalarText = (field: CatalogSchemaField, value: unknown) => {
    if (value === null || value === undefined || value === '') {
      return resolveEditControl(field, config.fields[field.key]) === 'switch'
        ? '未知'
        : '（空）'
    }
    if (typeof value === 'boolean') {
      return value ? '是' : '否'
    }
    if (field.vocabulary) {
      return vocabText(fieldVocabSlot(field), value)
    }
    const family = config.fields[field.key]?.ref
    if (field.field_type === 'ref' && family) {
      return refs.nameOf(family, value) || `#${value}`
    }
    return String(value)
  }

  const rowText = (fieldKey: string, row: unknown) => {
    const field = editor.fieldByKey.value.get(fieldKey)
    if (isSuppressedKey(fieldKey)) {
      const parts = suppressedKeyParts(String(row))
      if (parts.label) {
        return parts.label
      }
      const names = parts.refs.map(
        (ref) => refs.nameOf(ref.family, ref.id) || `#${ref.id}`
      )
      return names.length ? names.join(' · ') : String(row)
    }
    if (field?.element?.type === 'ref') {
      const family = config.fields[fieldKey]?.ref
      return family ? refs.nameOf(family, row) || `#${row}` : `#${row}`
    }
    if (!row || typeof row !== 'object') {
      return String(row ?? '')
    }
    if (fieldKey.endsWith('.roster')) {
      const r = row as Record<string, unknown>
      const members = field?.element?.members ?? []
      const kind = members.find((m) => m.key === 'kind')
      const spoiler = members.find((m) => m.key === 'spoiler')
      return [
        refs.nameOf('character', r.character_id) || `角色 #${r.character_id}`,
        kind ? memberText(fieldKey, kind, r.kind) : '',
        spoiler ? memberText(fieldKey, spoiler, r.spoiler) : ''
      ]
        .filter(Boolean)
        .join(' · ')
    }
    return (field?.element?.members ?? [])
      .map((member) =>
        memberText(
          fieldKey,
          member,
          (row as Record<string, unknown>)[member.key]
        )
      )
      .filter(Boolean)
      .join(' · ')
  }

  const refsInValue = (fieldKey: string, value: unknown) => {
    const field = editor.fieldByKey.value.get(fieldKey)
    const out: { family: EditRefFamily; id: string }[] = []
    const list = Array.isArray(value) ? value : [value]
    if (isSuppressedKey(fieldKey)) {
      for (const key of list) {
        out.push(...suppressedKeyParts(String(key)).refs)
      }
      return out
    }
    for (const row of list) {
      if (field?.element?.type === 'ref' && config.fields[fieldKey]?.ref) {
        out.push({ family: config.fields[fieldKey]!.ref!, id: String(row) })
        continue
      }
      if (!row || typeof row !== 'object') {
        continue
      }
      for (const member of field?.element?.members ?? []) {
        const family = config.fields[fieldKey]?.members?.[member.key]?.ref
        const id = (row as Record<string, unknown>)[member.key]
        if (
          member.type === 'ref' &&
          family &&
          id !== null &&
          id !== undefined
        ) {
          out.push({ family, id: String(id) })
        }
      }
      if (fieldKey.endsWith('.roster')) {
        out.push({
          family: 'character',
          id: String((row as Record<string, unknown>).character_id)
        })
      }
    }
    return out
  }

  return { rowText, scalarText, refsInValue, memberText }
}
