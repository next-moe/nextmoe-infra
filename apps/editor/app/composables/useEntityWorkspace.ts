import type { EditorImage } from './useEditorContext'
import { EDIT_CONFIGS, type EditFamily } from '~/constants/editEntities'

export const useEntityWorkspace = (
  family: EditFamily,
  entityId: MaybeRefOrGetter<string>,
  options: { persist?: boolean; readonly?: boolean } = {}
) => {
  const config = EDIT_CONFIGS[family]
  const id = computed(() => toValue(entityId))
  const editor = useEntityEditor(config, id, { persist: options.persist })
  const editContext = useEditContext(config, id, editor)
  const refs = useRefNames()
  const images = ref<Record<string, EditorImage>>({})

  provideEditorContext({
    editor,
    config,
    entityId: id,
    upstreamRows: editContext.upstreamRows,
    rosterIdentity: editContext.rosterIdentity,
    provenance: editContext.provenance,
    images,
    refs,
    readonly: options.readonly
  })

  const load = async () => {
    await Promise.all([editor.load(), editContext.load()])
    images.value = { ...editContext.images.value, ...images.value }
  }

  const groupOf = (key: string) => config.fields[key]?.group ?? 'other'

  const fieldsByGroup = computed(() => {
    const present = new Set(editor.fields.value.map((field) => field.key))
    const out: Record<string, string[]> = {}
    for (const field of editor.fields.value) {
      if (
        isSuppressedKey(field.key) &&
        present.has(field.key.slice(0, -'.suppressed'.length))
      ) {
        continue
      }
      const group = groupOf(field.key)
      out[group] = [...(out[group] ?? []), field.key]
    }
    return out
  })

  const groups = computed(() =>
    config.groups.filter((group) => fieldsByGroup.value[group.value]?.length)
  )

  return {
    config,
    editor,
    editContext,
    images,
    load,
    groupOf,
    fieldsByGroup,
    groups
  }
}
