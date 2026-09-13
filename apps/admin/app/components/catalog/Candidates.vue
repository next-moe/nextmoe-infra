<script setup lang="ts">
import {
  CATALOG_FILTER_ALL,
  CATALOG_ENTITY_TYPES,
  CATALOG_ENTITY_TYPE,
  CATALOG_QUEUE_SUMMARY_KEY,
  CANDIDATE_STATUS,
  CANDIDATE_STATUS_LABELS,
  CANDIDATE_REASON_LABELS
} from '~/constants/catalog'
import type {
  CatalogCandidateItem,
  CatalogCandidatePage,
  CatalogDecideCandidateData,
  CatalogDetachNameData,
  CatalogMergeDirection,
  CatalogQueueSummary
} from '~~/shared/types/catalog'

const api = useApi('catalog')

const page = ref(1)
const limit = 20
const status = ref(CANDIDATE_STATUS.needsManual as number)
const entityType = ref(CATALOG_ENTITY_TYPE.work as number)
const reason = ref(CATALOG_FILTER_ALL)
watch([status, entityType, reason], () => {
  page.value = 1
})

const { data: summary, refresh: refreshSummary } =
  await useApiFetch<CatalogQueueSummary>(
    '/admin/catalog/candidates/summary',
    { key: CATALOG_QUEUE_SUMMARY_KEY },
    'catalog'
  )

const {
  data,
  status: fetchStatus,
  refresh,
  error
} = await useApiFetch<CatalogCandidatePage>(
  '/admin/catalog/candidates',
  {
    query: computed(() => ({
      page: page.value,
      limit,
      status: status.value,
      entity_type: entityType.value,
      reason: reason.value
    }))
  },
  'catalog'
)
const items = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))
const isLoading = computed(() => fetchStatus.value === 'pending')

const statusTabs = computed(() => [
  { id: CATALOG_FILTER_ALL, label: '全部' },
  ...Object.entries(CANDIDATE_STATUS_LABELS).map(([id, label]) => ({
    id: Number(id),
    label
  }))
])

const entityTypeOptions = computed(() => [
  { value: CATALOG_FILTER_ALL, label: '全部类型' },
  ...Object.entries(CATALOG_ENTITY_TYPES).map(([id, label]) => ({
    value: Number(id),
    label
  }))
])
const reasonFilterOptions = computed(() => [
  { value: CATALOG_FILTER_ALL, label: '全部来由' },
  ...Object.entries(CANDIDATE_REASON_LABELS).map(([id, label]) => ({
    value: Number(id),
    label
  }))
])

const applyBucket = (nextEntityType: number, nextStatus: number) => {
  entityType.value = nextEntityType
  status.value = nextStatus
}

const isPersonLink = (c: CatalogCandidateItem) =>
  c.entity_type === CATALOG_ENTITY_TYPE.creditName

const keyOf = (c: CatalogCandidateItem) =>
  `${c.entity_type}:${c.a_id}:${c.b_id}`

const deciding = ref(false)

const afterWrite = async () => {
  await Promise.all([refresh(), refreshSummary()])
}

const decide = async (
  c: CatalogCandidateItem,
  action: 'accept' | 'reject' | 'defer',
  merge?: { direction: CatalogMergeDirection; note: string }
) => {
  deciding.value = true
  try {
    const body: Record<string, unknown> = {
      entity_type: c.entity_type,
      a_id: c.a_id,
      b_id: c.b_id,
      action,
      note: merge?.note || undefined
    }
    if (merge) {
      body.source_id = merge.direction === 'ab' ? c.a_id : c.b_id
      body.target_id = merge.direction === 'ab' ? c.b_id : c.a_id
    }
    const res = await api.post<CatalogDecideCandidateData>(
      '/admin/catalog/candidates/decide',
      body
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '操作失败', 'error')
      return false
    }
    if (action === 'accept' && isPersonLink(c)) {
      if (res.data?.needs_manual) {
        useKunMessage('双方已属不同 person，已标记待人工处理', 'warn')
      } else {
        useKunMessage(
          res.data?.person_created
            ? `已新建 person #${res.data?.person_id ?? ''}`
            : `已并入 person #${res.data?.person_id ?? ''}`,
          'success'
        )
      }
    } else if (action === 'accept') {
      useKunMessage(
        `已建合并提案 #${res.data?.proposal_id ?? ''}，约 48 小时冷静期后执行`,
        'success'
      )
    } else if (res.data?.released?.length) {
      useKunMessage(
        res.data.released.map((id) => `已放行 #${id}`).join('、'),
        'success'
      )
    } else {
      useKunMessage(
        action === 'reject' ? '已拒绝（永久保留为负知识）' : '已搁置',
        'success'
      )
    }
    await afterWrite()
    return true
  } finally {
    deciding.value = false
  }
}

const mergeOpen = ref(false)
const mergeTarget = ref<CatalogCandidateItem | null>(null)
const mergeDirection = ref<CatalogMergeDirection>('ab')

const askMerge = (
  c: CatalogCandidateItem,
  direction: CatalogMergeDirection
) => {
  mergeTarget.value = c
  mergeDirection.value = direction
  mergeOpen.value = true
}

const confirmMerge = async (note: string) => {
  const c = mergeTarget.value
  if (!c || deciding.value) return
  const ok = await decide(c, 'accept', {
    direction: mergeDirection.value,
    note
  })
  if (ok) mergeOpen.value = false
}

const releaseWork = async (workId: number) => {
  deciding.value = true
  try {
    const res = await api.post<{ work_id: number; status: number }>(
      '/admin/catalog/works/release',
      { work_id: workId }
    )
    if (res.code === 0) {
      useKunMessage(`已放行 #${workId}`, 'success')
      await afterWrite()
    } else {
      useKunMessage(res.message || '操作失败', 'error')
    }
  } finally {
    deciding.value = false
  }
}

const detachLink = async (c: CatalogCandidateItem) => {
  deciding.value = true
  try {
    for (const id of [c.a_id, c.b_id]) {
      const res = await api.post<CatalogDetachNameData>(
        '/admin/catalog/names/detach',
        { credit_name_id: id }
      )
      if (res.code !== 0) {
        useKunMessage(res.message || '撤销失败', 'error')
        return
      }
    }
    useKunMessage('已撤销关联（双方名义已摘离）', 'success')
    await afterWrite()
  } finally {
    deciding.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">目录人审队列</h1>
    <CatalogSubNav />

    <CatalogCandidatesOverview
      :summary="summary ?? null"
      :entity-type="entityType"
      :status="status"
      @select="applyBucket"
    />

    <div class="flex flex-wrap items-center gap-2">
      <KunButton
        v-for="tab in statusTabs"
        :key="tab.id"
        :color="status === tab.id ? 'primary' : 'default'"
        :variant="status === tab.id ? 'solid' : 'flat'"
        size="sm"
        @click="status = tab.id"
      >
        {{ tab.label }}
      </KunButton>
      <KunSelect
        v-model="entityType"
        :options="entityTypeOptions"
        aria-label="实体类型过滤"
        class="w-40"
      />
      <KunSelect
        v-model="reason"
        :options="reasonFilterOptions"
        aria-label="候选来由过滤"
        class="w-40"
      />
    </div>
    <p class="text-default-500 text-sm">
      当前筛选共 {{ total }} 条候选
      <span v-if="isLoading" class="text-default-400">· 加载中…</span>
    </p>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div class="space-y-3">
      <template v-for="c in items" :key="keyOf(c)">
        <CatalogCandidateCreditPair
          v-if="isPersonLink(c)"
          :item="c"
          :busy="deciding"
          @accept="decide(c, 'accept')"
          @reject="decide(c, 'reject')"
          @defer="decide(c, 'defer')"
          @detach="detachLink(c)"
        />
        <CatalogCandidatePair
          v-else
          :item="c"
          :busy="deciding"
          @merge="askMerge(c, $event)"
          @reject="decide(c, 'reject')"
          @defer="decide(c, 'defer')"
          @release="releaseWork"
        />
      </template>

      <KunCard v-if="!items.length && !error" content-class="p-10">
        <p class="text-default-400 text-center">
          {{ isLoading ? '加载中…' : '这一队是空的，换个格子看看' }}
        </p>
      </KunCard>
    </div>

    <KunPagination
      v-model:current-page="page"
      :total-page="totalPages"
      :is-loading="isLoading"
    />

    <CatalogMergeConfirm
      v-model="mergeOpen"
      :item="mergeTarget"
      :direction="mergeDirection"
      :busy="deciding"
      @confirm="confirmMerge"
    />
  </div>
</template>
