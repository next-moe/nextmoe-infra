import type { InjectionKey } from 'vue'
import type { CatalogColumnProvenance } from '#shared/types/catalog'
import type {
  EditEntityConfig,
  EditFieldConfig,
  EditUpstreamRow
} from '#shared/types/edit'
import type { EntityEditor } from './useEntityEditor'

export interface EditorImage {
  url: string
  thumbhash: string | null
}

export interface EditorContext {
  editor: EntityEditor
  config: EditEntityConfig
  entityId: ComputedRef<string>
  upstreamRows: ComputedRef<Record<string, EditUpstreamRow[]>>
  rosterIdentity: ComputedRef<Record<string, string>>
  provenance?: ComputedRef<Record<string, CatalogColumnProvenance[]>>
  images: Ref<Record<string, EditorImage>>
  refs: ReturnType<typeof useRefNames>
  readonly?: boolean
}

const EDITOR_CONTEXT: InjectionKey<EditorContext> = Symbol('editor-context')

export const provideEditorContext = (context: EditorContext) =>
  provide(EDITOR_CONTEXT, context)

export const useEditorContext = () => {
  const context = inject(EDITOR_CONTEXT)
  if (!context) {
    throw new Error('useEditorContext() called outside an entity editor')
  }
  return context
}

export const useEditorField = (fieldKey: MaybeRefOrGetter<string>) => {
  const context = useEditorContext()
  const key = computed(() => toValue(fieldKey))
  const field = computed(() => context.editor.fieldByKey.value.get(key.value))
  const config = computed<EditFieldConfig | undefined>(
    () => context.config.fields[key.value]
  )
  const value = computed({
    get: () => context.editor.draft.value[key.value],
    set: (next: unknown) => context.editor.setValue(key.value, next)
  })
  const baseValue = computed(() => context.editor.base.value[key.value])
  const companionKey = computed(() => suppressedKeyOf(key.value))
  const companion = computed(() =>
    context.editor.fieldByKey.value.get(companionKey.value)
  )
  const suppressed = computed<string[]>({
    get: () => {
      const raw = context.editor.draft.value[companionKey.value]
      return Array.isArray(raw) ? raw.map(String) : []
    },
    set: (next) => context.editor.setValue(companionKey.value, next)
  })
  const capability = computed(() =>
    context.editor.capabilityByKey.value.get(key.value)
  )
  const disabled = computed(
    () =>
      Boolean(context.readonly) ||
      !capability.value ||
      !capability.value.can_propose
  )
  const issues = computed(
    () => context.editor.issuesByKey.value[key.value] ?? []
  )

  const toggleSuppressed = (identity: string) => {
    const current = suppressed.value
    suppressed.value = current.includes(identity)
      ? current.filter((entry) => entry !== identity)
      : [...current, identity]
  }

  return {
    context,
    key,
    field,
    config,
    value,
    baseValue,
    companionKey,
    companion,
    suppressed,
    capability,
    disabled,
    issues,
    toggleSuppressed
  }
}
