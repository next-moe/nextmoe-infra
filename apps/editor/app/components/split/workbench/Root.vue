<script setup lang="ts">
const props = defineProps<{ mode: 'compose' | 'request' }>()

const route = useRoute()
const { load: loadSession } = useEditorSession()
callOnce('editor-session', loadSession)

const workId = computed(() => {
  const value = route.query.work
  return typeof value === 'string' && /^\d+$/.test(value) ? value : null
})

const workbench = createSplitWorkbench({
  mode: props.mode,
  workId,
  requestId: props.mode === 'request' ? String(route.params.id) : undefined
})
provideSplitWorkbench(workbench)
const { request, requestLoading, requestError, live, preview } = workbench

const title = computed(() =>
  props.mode === 'compose' ? '拆分作品' : `拆分请求 #${String(route.params.id)}`
)
useHead(() => ({ title: `${title.value} · 未萌编辑台` }))

const resultOpen = ref(false)
const submitOpen = ref(false)

const PANES = [
  { value: 'releases', textValue: '原作品的发行', icon: 'lucide:list-checks' },
  { value: 'draft', textValue: '新作品', icon: 'lucide:file-plus' },
  { value: 'result', textValue: '结果', icon: 'lucide:git-fork' }
]
const pane = ref('releases')
</script>

<template>
  <div
    class="bg-background text-foreground flex h-dvh flex-col overflow-hidden"
  >
    <SplitWorkbenchTopbar
      :title="title"
      @open-result="resultOpen = true"
      @open-submit="submitOpen = true"
    />

    <div v-if="requestLoading" class="mx-auto w-full max-w-5xl p-6">
      <MergeWorkbenchSkeleton />
    </div>
    <div v-else-if="requestError" class="mx-auto w-full max-w-3xl p-6">
      <KunInfo
        color="danger"
        variant="flat"
        icon="lucide:circle-x"
        title="无法打开这条拆分请求"
        :description="requestError"
      />
    </div>
    <div
      v-else-if="props.mode === 'compose' && !workId"
      class="mx-auto w-full max-w-3xl p-6"
    >
      <KunInfo
        color="warning"
        variant="flat"
        title="没有指定作品"
        description="从作品页的「拆分」打开这个工作台。"
      />
    </div>
    <SplitWorkbenchSummary v-else-if="request && !live" />
    <template v-else>
      <div class="border-default-200 shrink-0 border-b lg:hidden">
        <KunTab
          v-model="pane"
          :items="PANES"
          variant="underlined"
          name="split-panes"
          class-name="px-2"
        />
      </div>
      <div
        class="grid min-h-0 flex-1 lg:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)_minmax(0,1fr)]"
      >
        <SplitWorkbenchReleases
          :class="pane === 'releases' ? 'flex' : 'hidden lg:flex'"
        />
        <SplitWorkbenchDraft
          :class="pane === 'draft' ? 'flex' : 'hidden lg:flex'"
        />
        <SplitWorkbenchResult
          :class="pane === 'result' ? 'flex' : 'hidden lg:flex'"
        />
      </div>
    </template>

    <SplitWorkbenchResultModal v-if="preview" v-model="resultOpen" />
    <SplitWorkbenchSubmitDialog
      v-if="props.mode === 'compose'"
      v-model="submitOpen"
    />
  </div>
</template>
