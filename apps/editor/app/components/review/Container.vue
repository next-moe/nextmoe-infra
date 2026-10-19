<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type {
  CatalogProposal,
  CatalogProposalList,
  CatalogRevisionList
} from '#shared/types/catalog'
import { ENTITY_FAMILY, SITE_LABELS } from '~/constants/catalog'
import { conflictingFields } from '~/utils/review/history'
import {
  AGE_OPTIONS,
  countBy,
  filterQueue,
  needsAnalysis,
  runLimited,
  type QueueAnalysis,
  type QueueFilters
} from '~/utils/review/queue'

useHead({ title: '待审队列 · 未萌编辑台' })

const PAGE_SIZE = 30

const api = useEditorApi()
const names = useEntityNames()
const { isReviewer } = useEditorSession()
const queueIds = useState<string[]>('review-queue-ids', () => [])
const analyses = useState<Record<string, QueueAnalysis>>(
  'review-queue-analyses',
  () => ({})
)

const family = ref('')
const filters = reactive<QueueFilters>({
  site: '',
  age: '',
  field: '',
  conflictOnly: false
})
const items = ref<CatalogProposal[]>([])
const total = ref<number | null>(null)
const nextCursor = ref<string | null>(null)
const loading = ref(false)
const analysing = ref(false)
const selected = ref(0)

const familyOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部类型' },
  ...Object.entries(ENTITY_FAMILY).map(([value, { label }]) => ({
    value,
    label
  }))
]

const siteOptions = computed<KunSelectOption<string>[]>(() => [
  { value: '', label: '全部来源' },
  ...Object.entries(countBy(items.value, (item) => item.site)).map(
    ([site, count]) => ({
      value: site,
      label: `${labelOf(SITE_LABELS, site)} (${count})`
    })
  )
])

const ageOptions: KunSelectOption<string>[] = AGE_OPTIONS.map(
  ({ value, label }) => ({ value, label })
)

const fieldOptions = computed<KunSelectOption<string>[]>(() => {
  const counts = countBy(
    Object.entries(analyses.value)
      .filter(([id]) => items.value.some((item) => item.id === id))
      .flatMap(([, analysis]) => analysis.fields),
    (key) => key
  )
  return [
    { value: '', label: '全部字段' },
    ...Object.entries(counts)
      .sort((a, b) => b[1] - a[1])
      .map(([key, count]) => ({
        value: key,
        label: `${fieldLabel(key)} (${count})`
      }))
  ]
})

const visible = computed(() =>
  filterQueue(items.value, filters, analyses.value)
)

const analyse = async (proposal: CatalogProposal) => {
  if (analyses.value[proposal.id]) {
    return
  }
  const [detail, revisions] = await Promise.all([
    api<CatalogProposal>(`/v2/moderation/proposals/${proposal.id}`, {
      query: { include: 'patch' }
    }).catch(() => null),
    api<CatalogRevisionList>('/v2/catalog/revisions', {
      query: {
        object: proposal.target_object,
        entity_id: proposal.entity_id,
        limit: 100
      }
    }).catch(() => null)
  ])
  const fields = Object.keys(detail?.effective_patch ?? detail?.patch ?? {})
  analyses.value = {
    ...analyses.value,
    [proposal.id]: {
      fields,
      conflicts: conflictingFields(
        proposal.base_revision_seq,
        revisions?.items ?? [],
        fields
      )
    }
  }
}

const analyseLoaded = async () => {
  if (analysing.value) {
    return
  }
  analysing.value = true
  try {
    await runLimited(
      items.value.filter((item) => !analyses.value[item.id]),
      3,
      analyse
    )
  } finally {
    analysing.value = false
  }
}

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogProposalList>('/v2/moderation/proposals', {
      query: {
        limit: PAGE_SIZE,
        include_total: true,
        ...(family.value ? { object: family.value } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    items.value = cursor ? [...items.value, ...list.items] : list.items
    total.value = list.total ?? null
    nextCursor.value = list.next_cursor ?? null
    await names.resolve(list.items)
    if (needsAnalysis(filters)) {
      await analyseLoaded()
    }
  } catch {
    if (!cursor) {
      items.value = []
      total.value = null
    }
    nextCursor.value = null
  } finally {
    loading.value = false
  }
}

watch(family, () => fetchPage(null))
watch(
  () => needsAnalysis(filters),
  (needed) => {
    if (needed) {
      analyseLoaded()
    }
  }
)
watch(visible, (list) => {
  queueIds.value = list.map((item) => item.id)
  if (selected.value >= list.length) {
    selected.value = Math.max(list.length - 1, 0)
  }
})

const open = (index: number) => {
  const item = visible.value[index]
  if (item) {
    navigateTo(`/review/${item.id}`)
  }
}

const isTyping = (event: KeyboardEvent) => {
  const target = event.target as HTMLElement | null
  return (
    !!target &&
    (target.isContentEditable ||
      ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
  )
}

const onKey = (event: KeyboardEvent) => {
  if (event.metaKey || event.ctrlKey || event.altKey || isTyping(event)) {
    return
  }
  if (event.key === 'j') {
    selected.value = Math.min(selected.value + 1, visible.value.length - 1)
  } else if (event.key === 'k') {
    selected.value = Math.max(selected.value - 1, 0)
  } else if (event.key === 'Enter' || event.key === 'o') {
    open(selected.value)
  } else {
    return
  }
  event.preventDefault()
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

if (isReviewer.value) {
  await fetchPage(null)
}
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <KunInfo
      v-if="!isReviewer"
      color="warning"
      variant="flat"
      title="需要审核权限"
      description="待审队列只对审核者开放。想参与审核，可以先在提案里积累编辑记录。"
    />

    <template v-else>
      <div
        class="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between"
      >
        <KunHeader
          name="待审队列"
          :description="
            total === null
              ? '所有站点提交的待审提案。'
              : `所有站点共 ${total} 条待审提案。`
          "
        />
        <p class="text-default-400 text-xs">
          <kbd class="bg-default-100 rounded px-1">j</kbd>/<kbd
            class="bg-default-100 rounded px-1"
            >k</kbd
          >
          上下移动 ·
          <kbd class="bg-default-100 rounded px-1">Enter</kbd> 打开
        </p>
      </div>

      <KunCard bordered padding="md">
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <KunSelect
            v-model="family"
            :options="familyOptions"
            aria-label="条目类型"
          />
          <KunSelect
            v-model="filters.site"
            :options="siteOptions"
            aria-label="来源站点"
          />
          <KunSelect
            v-model="filters.age"
            :options="ageOptions"
            aria-label="提交时间"
          />
          <KunSelect
            v-model="filters.field"
            :options="fieldOptions"
            aria-label="涉及字段"
            :disabled="analysing"
          />
          <KunSwitch
            v-model="filters.conflictOnly"
            label="只看可能冲突"
            class-name="self-center"
          />
        </div>
        <p
          v-if="needsAnalysis(filters)"
          class="text-default-400 mt-2 flex items-center gap-1 text-xs"
        >
          <KunLoading v-if="analysing" spinner />
          按字段和冲突筛选会逐条读取已加载的提案{{
            analysing ? '，正在分析…' : '。'
          }}
        </p>
      </KunCard>

      <KunCard bordered padding="none">
        <ul v-if="visible.length" class="divide-default-200 divide-y">
          <ReviewQueueItem
            v-for="(proposal, index) in visible"
            :key="proposal.id"
            :proposal="proposal"
            :name="names.nameOf(proposal)"
            :analysis="analyses[proposal.id]"
            :selected="index === selected"
            @mouseenter="selected = index"
            @focusin="selected = index"
          />
        </ul>
        <KunNull
          v-else-if="!loading"
          :description="items.length ? '没有符合筛选条件的提案' : '队列已清空'"
        />
        <div class="flex justify-center p-3">
          <KunLoading v-if="loading" spinner />
          <KunButton
            v-else-if="nextCursor"
            variant="bordered"
            @click="fetchPage(nextCursor)"
          >
            加载更多
          </KunButton>
        </div>
      </KunCard>
    </template>
  </div>
</template>
