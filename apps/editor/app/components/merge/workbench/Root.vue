<script setup lang="ts">
import { MERGE_SECTIONS, type MergeSectionKey } from '~/constants/merge'
import {
  ENTITY_META,
  isEditFamily,
  type EditFamily
} from '~/constants/editEntities'

const props = defineProps<{ mode: 'compose' | 'request' }>()

const route = useRoute()
const router = useRouter()
const { load: loadSession } = useEditorSession()
callOnce('editor-session', loadSession)

const queryString = (key: string) => {
  const value = route.query[key]
  return typeof value === 'string' && /^\d+$/.test(value) ? value : null
}

const family = ref<EditFamily>(
  isEditFamily(String(route.query.object ?? ''))
    ? (String(route.query.object) as EditFamily)
    : 'work'
)
const source = computed(() => queryString('source'))
const target = computed(() => queryString('target'))

const workbench = createMergeWorkbench({
  mode: props.mode,
  family,
  source,
  target,
  requestId: props.mode === 'request' ? String(route.params.id) : undefined
})
provideMergeWorkbench(workbench)

const meta = computed(() => ENTITY_META[family.value])

const setPair = (next: { source?: string | null; target?: string | null }) => {
  router.replace({
    query: {
      ...route.query,
      object: family.value,
      source:
        next.source === undefined ? source.value : (next.source ?? undefined),
      target:
        next.target === undefined ? target.value : (next.target ?? undefined)
    }
  })
}

const sections = computed(() =>
  MERGE_SECTIONS.filter(
    (section) => family.value === 'work' || !section.worksOnly
  ).filter(
    (section) =>
      section.key === 'overview' ||
      (!!workbench.preview.value && !workbench.blocked.value)
  )
)

const active = ref<MergeSectionKey>('overview')
const scroller = ref<HTMLElement | null>(null)

const jump = (key: MergeSectionKey) => {
  active.value = key
  navOpen.value = false
  document
    .getElementById(`merge-section-${key}`)
    ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

let observer: IntersectionObserver | null = null
const observe = () => {
  observer?.disconnect()
  if (!scroller.value) {
    return
  }
  observer = new IntersectionObserver(
    (entries) => {
      const visible = entries
        .filter((entry) => entry.isIntersecting)
        .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
      const id = visible[0]?.target.id.replace('merge-section-', '')
      if (id) {
        active.value = id as MergeSectionKey
      }
    },
    { root: scroller.value, rootMargin: '-56px 0px -60% 0px' }
  )
  for (const section of sections.value) {
    const el = document.getElementById(`merge-section-${section.key}`)
    if (el) {
      observer.observe(el)
    }
  }
}
watch(
  () => sections.value.map((section) => section.key).join(','),
  () => nextTick(observe)
)
onMounted(() => nextTick(observe))
onBeforeUnmount(() => observer?.disconnect())

const navOpen = ref(false)
const resultOpen = ref(false)
const submitOpen = ref(false)

const title = computed(() =>
  props.mode === 'compose'
    ? `合并${meta.value.label}`
    : `合并请求 #${String(route.params.id)}`
)
useHead(() => ({ title: `${title.value} · 未萌编辑台` }))
</script>

<template>
  <div
    class="bg-background text-foreground flex h-dvh flex-col overflow-hidden"
  >
    <MergeWorkbenchTopbar
      :title="title"
      @open-nav="navOpen = true"
      @open-result="resultOpen = true"
      @open-submit="submitOpen = true"
    />

    <div class="flex min-h-0 flex-1">
      <aside
        class="border-default-200 bg-content1 hidden w-60 shrink-0 border-r lg:block"
      >
        <MergeWorkbenchRail
          :sections="sections"
          :active="active"
          @jump="jump"
        />
      </aside>

      <main ref="scroller" class="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <div class="mx-auto max-w-[1440px] space-y-10 px-4 pt-5 pb-24 md:px-8">
          <MergeWorkbenchSkeleton v-if="workbench.requestLoading.value" />
          <KunInfo
            v-else-if="workbench.requestError.value"
            color="danger"
            variant="flat"
            icon="lucide:circle-x"
            title="无法打开这条合并请求"
            :description="workbench.requestError.value"
          />
          <template v-else>
            <MergeWorkbenchOverview @set-pair="setPair" />
            <MergeWorkbenchSkeleton
              v-if="workbench.previewLoading.value && !workbench.preview.value"
              compact
            />
            <template v-else-if="sections.length > 1">
              <MergeWorkbenchColumns />
              <MergeWorkbenchFields
                v-if="sections.some((s) => s.key === 'fields')"
              />
              <MergeWorkbenchTitles
                v-if="sections.some((s) => s.key === 'titles')"
              />
              <MergeWorkbenchIntros
                v-if="sections.some((s) => s.key === 'intros')"
              />
              <MergeWorkbenchReleases
                v-if="sections.some((s) => s.key === 'releases')"
              />
              <MergeWorkbenchAnchors />
              <MergeWorkbenchCounts />
              <MergeWorkbenchUsers
                v-if="sections.some((s) => s.key === 'users')"
              />
              <MergeWorkbenchClaim
                v-if="sections.some((s) => s.key === 'claim')"
              />
            </template>
          </template>
        </div>
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
      <MergeWorkbenchRail :sections="sections" :active="active" @jump="jump" />
    </KunDrawer>

    <MergeWorkbenchResultModal v-model="resultOpen" />
    <MergeWorkbenchSubmitDialog
      v-if="props.mode === 'compose'"
      v-model="submitOpen"
    />
  </div>
</template>
