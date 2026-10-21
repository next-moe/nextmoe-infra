<script setup lang="ts">
import type { KunSelectOption } from '@kungal/ui-vue'
import type {
  CatalogProposal,
  CatalogProposalList
} from '#shared/types/catalog'
import { ENTITY_FAMILY, ORIGIN_SITE_LABELS } from '~/constants/catalog'
import { EDIT_CONFIGS, isEditFamily } from '~/constants/editEntities'
import {
  AGE_OPTIONS,
  filterQueue,
  type QueueAnalysis,
  type QueueFilters
} from '~/utils/review/queue'

useHead({ title: '待审队列 · 未萌编辑台' })

const PAGE_SIZE = 30

const SORT_OPTIONS: KunSelectOption<string>[] = [
  { value: 'filed_desc', label: '最新提交' },
  { value: 'filed_asc', label: '等待最久' },
  { value: 'updated_desc', label: '最近更新' }
]

const api = useEditorApi()
const names = useEntityNames()
const { isReviewer, settled } = useEditorSession()
const queueIds = useState<string[]>('review-queue-ids', () => [])

const family = ref('')
const sort = ref('filed_desc')
const filters = reactive<QueueFilters>({
  origin: '',
  age: '',
  field: '',
  conflictOnly: false
})
const items = ref<CatalogProposal[]>([])
const total = ref<number | null>(null)
const nextCursor = ref<string | null>(null)
const loading = ref(true)
const selected = ref(0)

const familyOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部类型' },
  ...Object.entries(ENTITY_FAMILY).map(([value, { label }]) => ({
    value,
    label
  }))
]

const originOptions: KunSelectOption<string>[] = [
  { value: '', label: '全部来源' },
  ...Object.entries(ORIGIN_SITE_LABELS).map(([value, label]) => ({
    value,
    label
  }))
]

const ageOptions: KunSelectOption<string>[] = AGE_OPTIONS.map(
  ({ value, label }) => ({ value, label })
)

const fieldOptions = computed<KunSelectOption<string>[]>(() => {
  const configs = isEditFamily(family.value)
    ? [EDIT_CONFIGS[family.value]]
    : Object.values(EDIT_CONFIGS)
  return [
    { value: '', label: '全部字段' },
    ...configs.flatMap((config) =>
      Object.keys(config.fields).map((key) => ({
        value: key,
        label: fieldLabel(key)
      }))
    )
  ]
})

const analyses = computed<Record<string, QueueAnalysis>>(() =>
  Object.fromEntries(
    items.value.map((item) => [
      item.id,
      {
        fields: item.changed_fields ?? [],
        conflicts: Object.fromEntries(
          (item.conflict_fields ?? []).map((key) => [key, 1])
        )
      }
    ])
  )
)

const visible = computed(() =>
  filterQueue(
    items.value,
    { origin: '', age: filters.age, field: '', conflictOnly: false },
    analyses.value
  )
)

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CatalogProposalList>('/v2/moderation/proposals', {
      query: {
        limit: PAGE_SIZE,
        include_total: true,
        include: 'patch',
        sort: sort.value,
        ...(family.value ? { object: family.value } : {}),
        ...(filters.origin ? { origin_site: filters.origin } : {}),
        ...(filters.field ? { field: filters.field } : {}),
        ...(filters.conflictOnly ? { has_conflict: true } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    items.value = cursor ? [...items.value, ...list.items] : list.items
    total.value = list.total ?? null
    nextCursor.value = list.next_cursor ?? null
    await names.resolve(list.items)
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

watch(family, () => {
  if (
    filters.field &&
    !fieldOptions.value.some((o) => o.value === filters.field)
  ) {
    filters.field = ''
  }
  fetchPage(null)
})
watch(
  () => [sort.value, filters.origin, filters.field, filters.conflictOnly],
  () => fetchPage(null)
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

watch(
  isReviewer,
  (reviewer) => {
    if (reviewer) {
      fetchPage(null)
    }
  },
  { immediate: true }
)
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <template v-if="!settled">
      <div class="space-y-2">
        <KunSkeleton variant="text" class-name="text-2xl" width="8rem" />
        <KunSkeleton variant="text" class-name="text-sm" width="16rem" />
      </div>
      <KunCard bordered padding="md">
        <CommonSkeletonRows :rows="6" trailing />
      </KunCard>
    </template>

    <KunInfo
      v-else-if="!isReviewer"
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
        <p class="text-default-400 flex items-center gap-1 text-xs">
          <KunKbd>j</KunKbd>/<KunKbd>k</KunKbd>
          上下移动 ·
          <KunKbd keys="Enter" /> 打开
        </p>
      </div>

      <KunCard bordered padding="md">
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <KunSelect
            v-model="family"
            :options="familyOptions"
            aria-label="条目类型"
          />
          <KunSelect
            v-model="filters.origin"
            :options="originOptions"
            aria-label="来源站点"
          />
          <KunSelect
            v-model="filters.field"
            :options="fieldOptions"
            :searchable="fieldOptions.length > 12"
            aria-label="涉及字段"
          />
          <KunSelect
            v-model="filters.age"
            :options="ageOptions"
            aria-label="提交时间"
          />
          <KunSelect v-model="sort" :options="SORT_OPTIONS" aria-label="排序" />
          <KunSwitch
            v-model="filters.conflictOnly"
            label="只看可能冲突"
            class-name="self-center"
          />
        </div>
      </KunCard>

      <KunCard bordered padding="none">
        <CommonSkeletonRows
          v-if="loading && !items.length"
          :rows="8"
          trailing
          class-name="px-4"
        />
        <ul v-else-if="visible.length" class="divide-default-200 divide-y">
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
          <KunLoading v-if="loading && items.length" spinner />
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
