import type { EffectScope, InjectionKey } from 'vue'
import type { CatalogProposal } from '#shared/types/catalog'
import type { EntityWorkspace } from './useEntityWorkspace'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const NAME_FIELDS: Partial<Record<EditFamily, string>> = {
  work: 'catalog.work.display_name',
  release: 'catalog.release.title',
  character: 'catalog.character.display_name',
  credit_name: 'catalog.credit_name.name',
  company: 'catalog.label.name',
  tag: 'catalog.tag.name',
  trait: 'catalog.trait.name',
  series: 'catalog.series.name',
  engine: 'catalog.engine.name'
}

export interface WorkspaceEntity {
  key: string
  family: EditFamily
  id: string
  workspace: EntityWorkspace
  label: Ref<string>
}

export interface WorkspaceSubmission {
  key: string
  family: EditFamily
  id: string
  label: string
  proposal: CatalogProposal
}

export const workspaceKey = (family: EditFamily, id: string) =>
  `${family}:${id}`

export const parentFieldKey = (key: string) =>
  isSuppressedKey(key) ? key.slice(0, -'.suppressed'.length) : key

export const changedParentKeys = (workspace: EntityWorkspace) => [
  ...new Set(workspace.editor.changedKeys.value.map(parentFieldKey))
]

export const createWorkspaceSession = (root: {
  family: EditFamily
  id: string
}) => {
  const nuxtApp = useNuxtApp()
  const entities = shallowReactive(new Map<string, WorkspaceEntity>())
  const scopes = new Map<string, EffectScope>()
  const submissions = ref<WorkspaceSubmission[]>([])

  const nameOf = (entity: WorkspaceEntity) => {
    const field = NAME_FIELDS[entity.family]
    const draft = field ? entity.workspace.editor.base.value[field] : null
    return (
      entity.label.value ||
      (typeof draft === 'string' && draft) ||
      `${ENTITY_META[entity.family].label} #${entity.id}`
    )
  }

  const open = (family: EditFamily, id: string, label?: string) => {
    const key = workspaceKey(family, id)
    const existing = entities.get(key)
    if (existing) {
      if (label && !existing.label.value) {
        existing.label.value = label
      }
      return existing
    }
    const scope = effectScope(true)
    const workspace = nuxtApp.runWithContext(() =>
      scope.run(() => createEntityWorkspace(family, id))!
    ) as EntityWorkspace
    const entity: WorkspaceEntity = {
      key,
      family,
      id,
      workspace,
      label: ref(label ?? '')
    }
    scopes.set(key, scope)
    entities.set(key, entity)
    workspace.load()
    return entity
  }

  const rootEntity = open(root.family, root.id)

  const list = computed(() => [...entities.values()])
  const dirty = computed(() =>
    list.value.filter((entity) => entity.workspace.editor.isDirty.value)
  )
  const changeCount = computed(() =>
    dirty.value.reduce(
      (sum, entity) => sum + changedParentKeys(entity.workspace).length,
      0
    )
  )
  const issueCount = computed(() =>
    dirty.value.reduce(
      (sum, entity) => sum + entity.workspace.editor.issues.value.length,
      0
    )
  )
  const blockedCount = computed(() =>
    dirty.value.reduce(
      (sum, entity) => sum + entity.workspace.editor.blockedKeys.value.length,
      0
    )
  )
  const isDirty = computed(() => dirty.value.length > 0)
  const canSubmit = computed(
    () => isDirty.value && issueCount.value === 0 && blockedCount.value === 0
  )
  const willAutomerge = computed(
    () =>
      isDirty.value &&
      dirty.value.every((entity) => entity.workspace.editor.willAutomerge.value)
  )

  const submit = async (entity: WorkspaceEntity, note: string) => {
    const result = await entity.workspace.editor.submit(note)
    if (result.ok) {
      submissions.value = [
        {
          key: entity.key,
          family: entity.family,
          id: entity.id,
          label: nameOf(entity),
          proposal: result.proposal
        },
        ...submissions.value
      ]
      entity.workspace.editor.acceptSubmitted()
      await entity.workspace.reload()
    }
    return result
  }

  const discardAll = () => {
    for (const entity of dirty.value) {
      entity.workspace.editor.resetAll()
    }
  }

  const dispose = () => {
    for (const scope of scopes.values()) {
      scope.stop()
    }
    scopes.clear()
    entities.clear()
  }

  return {
    root: rootEntity,
    entities,
    list,
    dirty,
    changeCount,
    issueCount,
    blockedCount,
    isDirty,
    canSubmit,
    willAutomerge,
    submissions,
    open,
    nameOf,
    submit,
    discardAll,
    dispose
  }
}

export type WorkspaceSession = ReturnType<typeof createWorkspaceSession>

const WORKSPACE_SESSION: InjectionKey<WorkspaceSession> =
  Symbol('workspace-session')

export const provideWorkspaceSession = (session: WorkspaceSession) =>
  provide(WORKSPACE_SESSION, session)

export const useWorkspaceSession = () => {
  const session = inject(WORKSPACE_SESSION)
  if (!session) {
    throw new Error('useWorkspaceSession() called outside an edit workspace')
  }
  return session
}
