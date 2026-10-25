<script setup lang="ts">
import type { CatalogWork, CatalogWorkCharacter } from '#shared/types/catalog'
import type { WalkthroughDocumentSource } from '~/composables/useWalkthroughDocument'
import { isMod, isTyping } from '~/utils/walkthrough/focus'

const GRAPH_WIDTH_KEY = 'nextmoe-editor-walkthrough-graph-width'

const props = defineProps<{
  source: WalkthroughDocumentSource
  entityId: string | null
  workId: string | null
  changeCount: number
  canSubmit: boolean
  submitHint: string
}>()

const emit = defineEmits<{
  submit: []
  changes: []
  discard: []
}>()

const api = useEditorApi()
const doc = createWalkthroughDocument(props.source)
provideWalkthroughDocument(doc)

const work = ref<CatalogWork | null>(null)
const roster = ref<CatalogWorkCharacter[]>([])
const rosterLoaded = ref(false)

const loadRoster = async (id: string) => {
  const out: CatalogWorkCharacter[] = []
  let cursor: string | null = null
  for (let page = 0; page < 10; page++) {
    const res: {
      items: CatalogWorkCharacter[]
      next_cursor?: string | null
    } | null = await api<{
      items: CatalogWorkCharacter[]
      next_cursor?: string | null
    }>(`/v2/catalog/works/${id}/characters`, {
      query: { nsfw: true, limit: 100, ...(cursor ? { cursor } : {}) }
    }).catch(() => null)
    out.push(...(res?.items ?? []))
    cursor = res?.next_cursor ?? null
    if (!cursor) break
  }
  return out
}

const loadWork = async (id: string | null) => {
  if (!id) return
  rosterLoaded.value = false
  const [summary, characters] = await Promise.all([
    api<CatalogWork>(`/v2/catalog/works/${id}`, {
      query: { nsfw: true }
    }).catch(() => null),
    loadRoster(id)
  ])
  work.value = summary
  roster.value = characters
  rosterLoaded.value = true
}
watch(() => props.workId, loadWork, { immediate: true })

const preferences = useEditorPreferences()
const workTitle = computed<string>(() => {
  const name = preferences.nameOf(work.value)
  return name || (props.workId ? `作品 #${props.workId}` : '')
})

const graphWidth = ref(520)
onMounted(() => {
  try {
    const stored = Number(localStorage.getItem(GRAPH_WIDTH_KEY))
    if (stored >= 320 && stored <= 1100) graphWidth.value = stored
  } catch {
    graphWidth.value = 520
  }
})
const rememberGraphWidth = (next: number) => {
  try {
    localStorage.setItem(GRAPH_WIDTH_KEY, String(next))
  } catch {
    return
  }
}

const pane = ref<'start' | 'end'>('start')
const shortcutsOpen = ref(false)
const railOpen = ref(false)
const issuesOpen = ref(false)

const trySubmit = () => {
  if (doc.issues.value.length) {
    issuesOpen.value = true
    return
  }
  emit('submit')
}

const onKeydown = (event: KeyboardEvent) => {
  const key = event.key.toLowerCase()
  if (isMod(event) && key === 'z') {
    event.preventDefault()
    if (event.shiftKey) doc.redo()
    else doc.undo()
    return
  }
  if (isMod(event) && key === 'y') {
    event.preventDefault()
    doc.redo()
    return
  }
  if (isMod(event) && key === 's') {
    event.preventDefault()
    trySubmit()
    return
  }
  if (!isTyping(event) && event.key === '?') {
    event.preventDefault()
    shortcutsOpen.value = true
    return
  }
  if (!isTyping(event) && event.key === 'Escape' && doc.selection.value) {
    doc.select(null)
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <WalkthroughsEditorTopbar
      v-model:issues-open="issuesOpen"
      v-model:pane="pane"
      :entity-id="props.entityId"
      :work-id="props.workId"
      :work-title="workTitle"
      :change-count="props.changeCount"
      :can-submit="props.canSubmit && !doc.issues.value.length"
      :submit-hint="props.submitHint"
      @submit="trySubmit"
      @changes="emit('changes')"
      @discard="emit('discard')"
      @shortcuts="shortcutsOpen = true"
      @rail="railOpen = true"
    />

    <div
      class="border-default-200 bg-content1 space-y-2 border-b px-4 py-3 empty:hidden md:px-6"
    >
      <slot name="notices" />
      <WalkthroughsEditorImportReview
        v-if="props.entityId"
        :entity-id="props.entityId"
      />
    </div>

    <KunInfo
      v-if="doc.readonly.value"
      color="warning"
      variant="flat"
      icon="lucide:lock"
      title="你目前只能查看这篇攻略"
      description="今天的提案额度已用完，或这篇攻略被锁定了。你仍然可以浏览全部内容。"
      class-name="m-3"
    />

    <div class="flex min-h-0 flex-1">
      <aside
        class="border-default-200 bg-content1 hidden w-72 shrink-0 border-r xl:block"
      >
        <WalkthroughsEditorRail
          :roster="roster"
          :roster-loaded="rosterLoaded"
          :hideable="props.entityId !== null"
        />
      </aside>

      <div class="min-w-0 flex-1">
        <KunSplitPane
          v-model:size="graphWidth"
          primary="end"
          :min-size="320"
          :max-size="1100"
          stack-below="lg"
          :show-pane="pane"
          aria-label="调整图预览宽度"
          @resize-end="rememberGraphWidth"
        >
          <template #start>
            <WalkthroughsEditorOutline />
          </template>
          <template #end>
            <WalkthroughsEditorGraph />
          </template>
        </KunSplitPane>
      </div>
    </div>

    <KunDrawer
      v-model="railOpen"
      placement="left"
      :responsive="false"
      size="sm"
      aria-label="角色、结局与存档"
      inner-class-name="p-0"
    >
      <WalkthroughsEditorRail
        :roster="roster"
        :roster-loaded="rosterLoaded"
        :hideable="props.entityId !== null"
      />
    </KunDrawer>

    <WalkthroughsEditorShortcuts v-model="shortcutsOpen" />
  </div>
</template>
