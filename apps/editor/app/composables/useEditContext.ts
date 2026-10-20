import type { CatalogEditContext } from '#shared/types/catalog'
import type {
  CatalogSchemaMember,
  EditEntityConfig,
  EditRefFamily,
  EditUpstreamRow
} from '#shared/types/edit'
import type { EntityEditor } from './useEntityEditor'
import { vocabLabel } from '~/constants/vocabLabels'

export const useEditContext = (
  config: EditEntityConfig,
  entityId: MaybeRefOrGetter<string>,
  editor: EntityEditor
) => {
  const api = useEditorApi()
  const refs = useRefNames()
  const { byName } = useVocabularies()
  const context = ref<CatalogEditContext | null>(null)

  const load = async () => {
    context.value = await api<CatalogEditContext>(
      `/v2/me/edit-context/${config.family}/${toValue(entityId)}`
    ).catch(() => null)
  }

  const fieldMap = computed(() => contextFieldMap(context.value))

  const memberText = (
    fieldKey: string,
    member: CatalogSchemaMember,
    value: unknown
  ) => {
    if (value === null || value === undefined || value === '') {
      return ''
    }
    const family = config.fields[fieldKey]?.members?.[member.key]?.ref
    if (member.type === 'ref') {
      return family ? refs.nameOf(family, value) || `#${value}` : `#${value}`
    }
    const slot = memberVocabSlot(member)
    if (slot) {
      const vocab = byName.value[slot.vocabulary]
      const token = vocabTokenOf(slot, vocab, value)
      if (!token) {
        return ''
      }
      const entry = vocab?.values.find((v) => v.value === token)
      return vocabLabel(slot.vocabulary, token, entry?.display_name)
    }
    if (member.type === 'bool') {
      return value
        ? (config.fields[fieldKey]?.members?.[member.key]?.label ?? '是')
        : ''
    }
    return String(value)
  }

  const upstreamRows = computed<Record<string, EditUpstreamRow[]>>(() => {
    const out: Record<string, EditUpstreamRow[]> = {}
    for (const field of editor.fields.value) {
      const rows = upstreamRowsOf(
        field.key,
        field,
        fieldMap.value.get(field.key),
        config.fields[field.key],
        (row) => rowIdentity(field.key, row),
        { memberText: (member, value) => memberText(field.key, member, value) }
      )
      if (rows.length) {
        out[field.key] = rows
      }
    }
    return out
  })

  const rosterIdentity = computed(() =>
    contextRosterIdentity(fieldMap.value.get(`${config.entityType}.roster`))
  )
  const provenance = computed(() => contextProvenance(context.value))
  const images = computed(() =>
    contextImages(editor.fields.value, editor.base.value, context.value)
  )

  watch([context, editor.fields], () => {
    const byFamily = new Map<EditRefFamily, Set<string>>()
    for (const field of editor.fields.value) {
      for (const row of fieldMap.value.get(field.key)?.rows ?? []) {
        if (!row.value || typeof row.value !== 'object') {
          continue
        }
        for (const member of field.element?.members ?? []) {
          const family = config.fields[field.key]?.members?.[member.key]?.ref
          const id = (row.value as Record<string, unknown>)[member.key]
          if (member.type === 'ref' && family && id != null) {
            byFamily.set(
              family,
              (byFamily.get(family) ?? new Set()).add(String(id))
            )
          }
        }
      }
    }
    for (const [family, ids] of byFamily) {
      refs.resolve(family, [...ids])
    }
  })

  return { context, load, upstreamRows, rosterIdentity, provenance, images }
}

export type EditContextState = ReturnType<typeof useEditContext>
