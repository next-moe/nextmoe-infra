<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type {
  CatalogEntitySummary,
  CatalogProposal,
  CatalogWork
} from '#shared/types/catalog'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const RAIL_KEY = 'nextmoe-editor-workspace-rail'

const props = defineProps<{ family: EditFamily }>()

const route = useRoute()
const router = useRouter()
const api = useEditorApi()
const { load: loadSession } = useEditorSession()
const { capture } = useReturnLink()

capture()
callOnce('editor-session', loadSession)

const entityId = String(route.params.id)
const meta = ENTITY_META[props.family]

const session = createWorkspaceSession({ family: props.family, id: entityId })
provideWorkspaceSession(session)
onBeforeUnmount(session.dispose)

const workspace = session.root.workspace
const editor = workspace.editor
provideEditorContext(workspace.context)

const { data: summary } = useLazyAsyncData(
  `workspace-summary-${props.family}-${entityId}`,
  () =>
    api<CatalogEntitySummary & Partial<CatalogWork>>(
      `/v2/catalog/${meta.collection}/${entityId}`,
      {
        query: {
          nsfw: true,
          ...(props.family === 'work' ? {} : { view: 'full' })
        }
      }
    ).catch(() => null)
)

const name = computed(
  () =>
    summary.value?.display_name ||
    summary.value?.title ||
    session.nameOf(session.root)
)
const thumbnail = computed(() => {
  const value = summary.value
  if (!value) {
    return null
  }
  const picture = value.cover ?? value.image ?? value.photo ?? value.logo
  return picture?.url
    ? { url: picture.url, thumbhash: picture.thumbhash ?? null }
    : null
})

const { sections, grouped, sectionOfField, changesBySection } =
  useWorkspaceSections(workspace)

const sectionQuery = useWorkspaceQuery('section')
const itemQuery = useWorkspaceQuery('item')

const go = (key: string, item: string | null = null) => {
  router.replace({
    query: {
      ...route.query,
      section: key === sections.value[0]?.key ? undefined : key,
      item: item ?? undefined
    }
  })
}

const section = computed<string>({
  get: () =>
    sections.value.some((entry) => entry.key === sectionQuery.value)
      ? sectionQuery.value!
      : (sections.value[0]?.key ?? ''),
  set: (key) => go(key)
})
const currentSection = computed(() =>
  sections.value.find((entry) => entry.key === section.value)
)

const jumpToField = async (key: string) => {
  const target = sectionOfField(key)
  if (target && target.key !== section.value) {
    go(target.key)
  }
  await nextTick()
  await nextTick()
  document
    .getElementById(`field-${key}`)
    ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

provideWorkspaceNav({
  section,
  item: itemQuery,
  sections,
  changesBySection,
  go,
  jumpToField
})

const relatedCounts = useWorkRelatedCounts(workspace)
const counts = computed<Record<string, number | null>>(() => {
  const lengthOf = (key: string) => {
    const value = editor.draft.value[key]
    return Array.isArray(value) ? value.length : null
  }
  return {
    titles: lengthOf(`catalog.${props.family}.titles`),
    roster: lengthOf(`catalog.${props.family}.roster`),
    ...relatedCounts.value
  }
})

const collapsed = ref(false)
onMounted(() => {
  try {
    collapsed.value = localStorage.getItem(RAIL_KEY) === '1'
  } catch {
    collapsed.value = false
  }
})
const toggleCollapsed = () => {
  collapsed.value = !collapsed.value
  try {
    localStorage.setItem(RAIL_KEY, collapsed.value ? '1' : '0')
  } catch {
    return
  }
}

const navOpen = ref(false)
watch(section, () => {
  navOpen.value = false
})

const changesOpen = ref(false)
const submitOpen = ref(false)

const openSubmit = () => {
  if (session.canSubmit.value) {
    submitOpen.value = true
  } else if (session.isDirty.value) {
    changesOpen.value = true
  }
}

const onKeydown = (event: KeyboardEvent) => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    openSubmit()
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

useUnsavedGuard(session.isDirty)

const canEditAnything = computed(() =>
  (editor.capabilities.value?.fields ?? []).some((cap) => cap.can_propose)
)

const submissionsShown = ref(true)
watch(
  () => session.submissions.value.length,
  () => {
    submissionsShown.value = true
  }
)

const prefillFromProposal = async (proposalId: string) => {
  const proposal = await api<CatalogProposal>(
    `/v2/me/proposals/${proposalId}`,
    { query: { include: 'patch' } }
  ).catch(() => null)
  const patch = (proposal?.effective_patch ?? proposal?.patch) as
    Record<string, unknown> | undefined
  if (
    proposal &&
    proposal.target_object === props.family &&
    String(proposal.entity_id) === entityId &&
    patch
  ) {
    editor.overlay(patch)
    useKunMessage(
      `已载入提案 #${proposalId} 的修改，可以继续调整后重新提交`,
      'info'
    )
  }
}

workspace.load().then(() => {
  const fromProposal = route.query.from_proposal
  if (typeof fromProposal === 'string' && /^\d+$/.test(fromProposal)) {
    prefillFromProposal(fromProposal)
  }
})

useHead(() => ({
  title: `编辑 ${name.value} · 未萌编辑台`
}))
</script>

<template>
  <div
    class="bg-background text-foreground flex h-dvh flex-col overflow-hidden"
  >
    <WorkspaceTopbar
      :family="props.family"
      :entity-id="entityId"
      :name="name"
      @open-nav="navOpen = true"
      @open-changes="changesOpen = true"
      @open-submit="openSubmit"
    />

    <div class="flex min-h-0 flex-1">
      <aside
        :class="
          cn(
            'border-default-200 bg-content1 hidden shrink-0 border-r transition-[width] duration-200 lg:block',
            collapsed ? 'w-14' : 'w-60'
          )
        "
      >
        <WorkspaceRail
          v-model="section"
          :family="props.family"
          :name="name"
          :thumbnail="thumbnail"
          :groups="grouped"
          :changes="changesBySection"
          :counts="counts"
          :loading="editor.loading.value"
          :collapsed="collapsed"
          @toggle-collapse="toggleCollapsed"
        />
      </aside>

      <main class="flex min-h-0 min-w-0 flex-1 flex-col">
        <div
          v-if="editor.loading.value"
          class="mx-auto w-full max-w-5xl space-y-5 overflow-hidden px-4 py-5 md:px-8 md:py-6"
        >
          <KunSkeleton variant="text" width="8rem" class-name="text-lg" />
          <WorkspaceSkeletonForm />
        </div>

        <div v-else-if="editor.loadError.value" class="p-6">
          <KunInfo
            :color="editor.loadStatus.value === 404 ? 'default' : 'danger'"
            variant="flat"
            :icon="
              editor.loadStatus.value === 404
                ? 'lucide:construction'
                : 'lucide:circle-x'
            "
            :title="
              editor.loadStatus.value === 404 && !editor.schema.value
                ? `${meta.label}暂未开放编辑`
                : '无法打开编辑器'
            "
            :description="editor.loadError.value"
          />
        </div>

        <template v-else>
          <div
            v-if="
              editor.storedDraft.value ||
              !canEditAnything ||
              (submissionsShown && session.submissions.value.length)
            "
            class="border-default-200 bg-content1 space-y-2 border-b px-4 py-3 md:px-6"
          >
            <WorkspaceDraftNotice :workspace="workspace" />
            <KunInfo
              v-if="!canEditAnything"
              color="warning"
              variant="flat"
              icon="lucide:lock"
              :title="
                editor.capabilities.value?.quota_exhausted
                  ? '今天的提案额度已用完'
                  : `你目前不能修改这个${meta.label}`
              "
              :description="
                editor.capabilities.value?.quota_exhausted
                  ? '额度在滚动窗口内逐步恢复，稍后再来。你仍然可以查看全部内容。'
                  : '可以查看全部内容；需要修改时请联系审核者。'
              "
            />
            <div
              v-if="submissionsShown && session.submissions.value.length"
              class="bg-success-50 text-success-800 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg px-3 py-2 text-sm"
            >
              <KunIcon name="lucide:circle-check" class="size-4 shrink-0" />
              <span>本次已提交：</span>
              <NuxtLink
                v-for="entry in session.submissions.value"
                :key="entry.proposal.id"
                :to="`/me/proposals/${entry.proposal.id}`"
                target="_blank"
                class="hover:underline"
              >
                {{ entry.label }}（{{
                  entry.proposal.state === 'merged' ? '已生效' : '待审核'
                }}）
              </NuxtLink>
              <span class="flex-1" />
              <button
                type="button"
                class="text-success-700 hover:bg-success-100 rounded p-0.5"
                aria-label="关闭"
                @click="submissionsShown = false"
              >
                <KunIcon name="lucide:x" class="size-4" />
              </button>
            </div>
          </div>
          <div class="min-h-0 flex-1">
            <WorkspaceSectionView
              v-if="currentSection"
              :section="currentSection"
            />
          </div>
        </template>
      </main>
    </div>

    <KunDrawer
      v-model="navOpen"
      placement="left"
      :responsive="false"
      size="sm"
      aria-label="分区导航"
      inner-class-name="p-0"
    >
      <WorkspaceRail
        v-model="section"
        :family="props.family"
        :name="name"
        :thumbnail="thumbnail"
        :groups="grouped"
        :changes="changesBySection"
        :counts="counts"
        :loading="editor.loading.value"
      />
    </KunDrawer>

    <WorkspaceChangesModal v-model="changesOpen" />
    <WorkspaceSubmitDialog v-model="submitOpen" />
  </div>
</template>
