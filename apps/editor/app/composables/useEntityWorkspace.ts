import type { EditorContext, EditorImage } from './useEditorContext'
import { EDIT_CONFIGS, type EditFamily } from '~/constants/editEntities'

export interface EntityWorkspaceOptions {
  persist?: boolean
  readonly?: boolean
}

export const createEntityWorkspace = (
  family: EditFamily,
  entityId: MaybeRefOrGetter<string>,
  options: EntityWorkspaceOptions = {}
) => {
  const config = EDIT_CONFIGS[family]
  const id = computed(() => toValue(entityId))
  const editor = useEntityEditor(config, id, { persist: options.persist })
  const editContext = useEditContext(config, id, editor)
  const refs = useRefNames()
  const images = ref<Record<string, EditorImage>>({})
  const loaded = ref(false)

  const context: EditorContext = {
    editor,
    config,
    entityId: id,
    upstreamRows: editContext.upstreamRows,
    rosterIdentity: editContext.rosterIdentity,
    provenance: editContext.provenance,
    images,
    refs,
    readonly: options.readonly
  }

  let pending: Promise<void> | null = null

  const load = () => {
    pending ??= Promise.all([editor.load(), editContext.load()]).then(() => {
      images.value = { ...editContext.images.value, ...images.value }
      loaded.value = true
    })
    return pending
  }

  const reload = () => {
    pending = null
    return load()
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
    family,
    id,
    config,
    editor,
    editContext,
    images,
    context,
    loaded,
    load,
    reload,
    groupOf,
    fieldsByGroup,
    groups
  }
}

export type EntityWorkspace = ReturnType<typeof createEntityWorkspace>

export const useEntityWorkspace = (
  family: EditFamily,
  entityId: MaybeRefOrGetter<string>,
  options: EntityWorkspaceOptions = {}
) => {
  const workspace = createEntityWorkspace(family, entityId, options)
  provideEditorContext(workspace.context)
  return workspace
}
