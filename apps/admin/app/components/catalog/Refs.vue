<script setup lang="ts">
import {
  CATALOG_FILTER_ALL,
  CATALOG_ENTITY_TYPES,
  CATALOG_QUEUE_SUMMARY_KEY,
  CATALOG_SOURCE_LABELS,
  MATCHED_BY_KINDS,
  catalogExternalUrl
} from '~/constants/catalog'
import type {
  CatalogProbableRefItem,
  CatalogProbableRefPage,
  CatalogQueueSummary,
  CatalogRefActionData
} from '~~/shared/types/catalog'

const api = useApi('catalog')

const page = ref(1)
const limit = 50
const sourceID = ref(CATALOG_FILTER_ALL)
const entityType = ref(CATALOG_FILTER_ALL)
watch([sourceID, entityType], () => {
  page.value = 1
})

const { data: summary, refresh: refreshSummary } =
  await useApiFetch<CatalogQueueSummary>(
    '/admin/catalog/candidates/summary',
    { key: CATALOG_QUEUE_SUMMARY_KEY },
    'catalog'
  )

const entityBuckets = computed(() =>
  [...(summary.value?.probable_refs ?? [])].sort((a, b) => b.count - a.count)
)

const {
  data,
  status: fetchStatus,
  refresh,
  error
} = await useApiFetch<CatalogProbableRefPage>(
  '/admin/catalog/refs/probable',
  {
    query: computed(() => ({
      page: page.value,
      limit,
      source_id: sourceID.value,
      entity_type: entityType.value
    }))
  },
  'catalog'
)
const items = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))
const isLoading = computed(() => fetchStatus.value === 'pending')

const sourceOptions = computed(() => [
  { value: CATALOG_FILTER_ALL, label: '全部来源' },
  ...Object.entries(CATALOG_SOURCE_LABELS).map(([id, label]) => ({
    value: Number(id),
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

const matchedByKind = (matchedBy: string) =>
  MATCHED_BY_KINDS.find((k) => matchedBy.startsWith(k.prefix))

const externalUrl = (r: CatalogProbableRefItem) =>
  catalogExternalUrl(r.source_id, r.external_id, r.entity_type)

const keyBody = (r: CatalogProbableRefItem) => ({
  entity_type: r.entity_type,
  entity_id: r.entity_id,
  source_id: r.source_id,
  external_id: r.external_id
})

const refKey = (r: CatalogProbableRefItem) =>
  `${r.entity_type}:${r.entity_id}:${r.source_id}:${r.external_id}`

const acting = ref('')
const conflictHint = ref('')

const afterWrite = async () => {
  await Promise.all([refresh(), refreshSummary()])
}

const confirmRef = async (r: CatalogProbableRefItem) => {
  acting.value = refKey(r)
  conflictHint.value = ''
  try {
    const res = await api.post<CatalogRefActionData>(
      '/admin/catalog/refs/confirm',
      keyBody(r)
    )
    if (res.code === 0) {
      useKunMessage('已确认为 exact', 'success')
      await afterWrite()
    } else {
      useKunMessage(res.message || '确认失败', 'error')
      conflictHint.value = `${res.message}。若两者可能是同一实体，请到候选桶为它们建立候选后走合并流程。`
    }
  } finally {
    acting.value = ''
  }
}

const rejectOpen = ref(false)
const rejectTarget = ref<CatalogProbableRefItem | null>(null)
const rejectReason = ref('')

const askReject = (r: CatalogProbableRefItem) => {
  rejectTarget.value = r
  rejectReason.value = ''
  rejectOpen.value = true
}

const confirmReject = async () => {
  const r = rejectTarget.value
  if (!r || acting.value) return
  if (!rejectReason.value.trim()) {
    useKunMessage('拒绝必须填写理由（它会成为永久负知识）', 'warn')
    return
  }
  acting.value = refKey(r)
  try {
    const res = await api.post<CatalogRefActionData>(
      '/admin/catalog/refs/reject',
      {
        ...keyBody(r),
        reason: rejectReason.value.trim()
      }
    )
    if (res.code === 0) {
      useKunMessage('已拒绝并记录负知识', 'success')
      rejectOpen.value = false
      await afterWrite()
    } else {
      useKunMessage(res.message || '拒绝失败', 'error')
    }
  } finally {
    acting.value = ''
  }
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">目录人审队列</h1>
    <CatalogSubNav />

    <div v-if="entityBuckets.length" class="flex flex-wrap items-center gap-2">
      <span class="text-default-500 text-sm">待确认分布：</span>
      <KunButton
        :color="entityType === CATALOG_FILTER_ALL ? 'primary' : 'default'"
        :variant="entityType === CATALOG_FILTER_ALL ? 'solid' : 'flat'"
        size="xs"
        @click="entityType = CATALOG_FILTER_ALL"
      >
        全部
      </KunButton>
      <KunButton
        v-for="bucket in entityBuckets"
        :key="bucket.entity_type"
        :color="entityType === bucket.entity_type ? 'primary' : 'default'"
        :variant="entityType === bucket.entity_type ? 'solid' : 'flat'"
        size="xs"
        @click="entityType = bucket.entity_type"
      >
        {{ CATALOG_ENTITY_TYPES[bucket.entity_type] ?? bucket.entity_type }}
        <span class="ml-1 font-mono">{{ bucket.count }}</span>
      </KunButton>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <KunSelect
        v-model="sourceID"
        :options="sourceOptions"
        aria-label="来源过滤"
        class="w-40"
      />
      <KunSelect
        v-model="entityType"
        :options="entityTypeOptions"
        aria-label="实体类型过滤"
        class="w-40"
      />
    </div>
    <p class="text-default-500 text-sm">共 {{ total }} 条待确认</p>
    <p v-if="conflictHint" class="text-warning-600 text-sm">
      {{ conflictHint }}
    </p>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
      <table class="w-full min-w-[56rem] text-sm">
        <thead class="bg-content2 text-default-500">
          <tr>
            <th class="px-3 py-2 text-left font-medium">实体</th>
            <th class="px-3 py-2 text-left font-medium">来源</th>
            <th class="px-3 py-2 text-left font-medium">外部 ID</th>
            <th class="px-3 py-2 text-left font-medium">断言来由</th>
            <th class="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="r in items"
            :key="refKey(r)"
            class="border-default-200 border-t align-top"
          >
            <td class="px-3 py-2">
              <KunChip color="info" variant="flat" size="xs" class="mr-1">
                {{ CATALOG_ENTITY_TYPES[r.entity_type] ?? r.entity_type }}
              </KunChip>
              <span class="text-foreground">
                {{ r.entity.display_name || `#${r.entity_id}` }}
              </span>
              <span class="text-default-400 ml-1 font-mono text-xs">
                #{{ r.entity_id }}
              </span>
            </td>
            <td class="px-3 py-2">
              {{ CATALOG_SOURCE_LABELS[r.source_id] ?? r.source_id }}
            </td>
            <td class="px-3 py-2">
              <KunLink
                v-if="externalUrl(r)"
                :href="externalUrl(r) || ''"
                target="_blank"
                underline="hover"
                color="primary"
                class-name="font-mono"
                is-show-anchor-icon
              >
                {{ r.external_id }}
              </KunLink>
              <span v-else class="font-mono">{{ r.external_id }}</span>
              <KunCopy :text="r.external_id" />
            </td>
            <td class="px-3 py-2">
              <KunChip
                :color="matchedByKind(r.matched_by)?.color ?? 'default'"
                variant="flat"
                size="xs"
              >
                {{ matchedByKind(r.matched_by)?.label ?? '未知' }}
              </KunChip>
              <span class="text-default-400 ml-1 font-mono text-xs">
                {{ r.matched_by }}
              </span>
            </td>
            <td class="px-3 py-2 text-right">
              <div class="flex justify-end gap-2">
                <KunButton
                  color="success"
                  variant="flat"
                  size="sm"
                  :disabled="acting === refKey(r)"
                  @click="confirmRef(r)"
                >
                  确认 exact
                </KunButton>
                <KunButton
                  color="danger"
                  variant="flat"
                  size="sm"
                  @click="askReject(r)"
                >
                  拒绝
                </KunButton>
              </div>
            </td>
          </tr>
          <tr v-if="!items.length && !error">
            <td colspan="5" class="text-default-400 px-3 py-10 text-center">
              {{ isLoading ? '加载中…' : '没有待确认的外部链接' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <KunPagination
      v-model:current-page="page"
      :total-page="totalPages"
      :is-loading="isLoading"
    />

    <KunModal v-model="rejectOpen" aria-label="拒绝外部链接">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">拒绝外部链接</h2>
        <p class="text-default-500 text-sm">
          将删除
          <span class="text-foreground font-medium">
            {{
              rejectTarget?.entity.display_name || `#${rejectTarget?.entity_id}`
            }}
          </span>
          ↔
          <span class="font-mono">{{ rejectTarget?.external_id }}</span>
          的断言，并记录为永久负知识（导入永不复活该配对）。
        </p>
        <KunInput
          v-model="rejectReason"
          placeholder="拒绝理由（必填，写明为什么是错误配对）"
        />
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="rejectOpen = false">
            取消
          </KunButton>
          <KunButton
            color="danger"
            :disabled="!rejectReason.trim() || acting !== ''"
            @click="confirmReject"
          >
            确认拒绝
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
