import type { InjectionKey } from 'vue'
import type { EntityWorkspace } from './useEntityWorkspace'
import {
  FAMILY_SECTIONS,
  WORKSPACE_GROUP_LABELS,
  WORKSPACE_GROUP_ORDER,
  sectionsFromGroups,
  type WorkspaceSectionDef,
  type WorkspaceSectionGroup
} from '~/constants/workspace'

const OTHER_SECTION: Omit<WorkspaceSectionDef, 'fields'> = {
  key: 'other',
  label: '其他',
  icon: 'lucide:ellipsis',
  group: 'other',
  guide: 'work-basics'
}

export const useWorkspaceSections = (workspace: EntityWorkspace) => {
  const sections = computed<WorkspaceSectionDef[]>(() => {
    const preset = FAMILY_SECTIONS[workspace.family]
    if (!preset) {
      return sectionsFromGroups(
        workspace.family,
        workspace.groups.value,
        workspace.fieldsByGroup.value
      )
    }
    const present = new Set(
      workspace.editor.fields.value.map((field) => field.key)
    )
    const claimed = new Set(preset.flatMap((section) => section.fields))
    const leftovers = [...present].filter(
      (key) => !isSuppressedKey(key) && !claimed.has(key)
    )
    return [
      ...preset
        .filter(
          (section) =>
            section.always || section.fields.some((key) => present.has(key))
        )
        .map((section) => ({
          ...section,
          fields: section.fields.filter((key) => present.has(key))
        })),
      ...(leftovers.length ? [{ ...OTHER_SECTION, fields: leftovers }] : [])
    ]
  })

  const grouped = computed(() =>
    WORKSPACE_GROUP_ORDER.map((group) => ({
      group,
      label: WORKSPACE_GROUP_LABELS[group],
      sections: sections.value.filter((section) => section.group === group)
    })).filter((entry) => entry.sections.length > 0)
  )

  const sectionOfField = (key: string) => {
    const parent = parentFieldKey(key)
    return sections.value.find((section) => section.fields.includes(parent))
  }

  const changesBySection = computed(() => {
    const out: Record<string, number> = {}
    for (const key of changedParentKeys(workspace)) {
      const section = sectionOfField(key)
      if (section) {
        out[section.key] = (out[section.key] ?? 0) + 1
      }
    }
    return out
  })

  return { sections, grouped, sectionOfField, changesBySection }
}

export type WorkspaceGroupEntry = {
  group: WorkspaceSectionGroup
  label: string
  sections: WorkspaceSectionDef[]
}

export const useWorkspaceQuery = (key: string) => {
  const route = useRoute()
  const router = useRouter()
  return computed<string | null>({
    get: () => {
      const raw = route.query[key]
      return typeof raw === 'string' && raw ? raw : null
    },
    set: (next) => {
      if ((route.query[key] ?? null) === next) {
        return
      }
      router.replace({ query: { ...route.query, [key]: next ?? undefined } })
    }
  })
}

export interface WorkspaceNav {
  section: Ref<string>
  item: Ref<string | null>
  sections: ComputedRef<WorkspaceSectionDef[]>
  changesBySection: ComputedRef<Record<string, number>>
  go: (section: string, item?: string | null) => void
  jumpToField: (key: string) => void
}

const WORKSPACE_NAV: InjectionKey<WorkspaceNav> = Symbol('workspace-nav')

export const provideWorkspaceNav = (nav: WorkspaceNav) =>
  provide(WORKSPACE_NAV, nav)

export const useWorkspaceNav = () => {
  const nav = inject(WORKSPACE_NAV)
  if (!nav) {
    throw new Error('useWorkspaceNav() called outside an edit workspace')
  }
  return nav
}
