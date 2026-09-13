<script setup lang="ts">
import {
  CATALOG_FILTER_ALL,
  CATALOG_ENTITY_TYPES,
  PROPOSAL_STATUS,
  PROPOSAL_STATUS_LABELS,
  PROPOSAL_STATUS_COLORS
} from '~/constants/catalog'
import type {
  CatalogProposalItem,
  CatalogProposalPage,
  CatalogProposalActionData
} from '~~/shared/types/catalog'

const api = useApi('catalog')

const page = ref(1)
const limit = 50
const status = ref(PROPOSAL_STATUS.open as number)
const entityType = ref(CATALOG_FILTER_ALL)
watch([status, entityType], () => {
  page.value = 1
})

const { data, status: fetchStatus, refresh, error } =
  await useApiFetch<CatalogProposalPage>(
    '/admin/catalog/proposals',
    {
      query: computed(() => ({
        page: page.value,
        limit,
        status: status.value,
        entity_type: entityType.value
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
  ...Object.entries(PROPOSAL_STATUS_LABELS).map(([id, label]) => ({
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

const now = ref(Date.now())
let tick: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  tick = setInterval(() => {
    now.value = Date.now()
  }, 10_000)
})
onUnmounted(() => {
  if (tick) clearInterval(tick)
})

const coolingRemaining = (p: CatalogProposalItem): string | null => {
  if (p.status !== PROPOSAL_STATUS.approved || !p.execute_after) return null
  const ms = new Date(p.execute_after).getTime() - now.value
  if (ms <= 0) return null
  const h = Math.floor(ms / 3_600_000)
  const m = Math.ceil((ms % 3_600_000) / 60_000)
  return `${h} 小时 ${m} 分`
}

const fieldResolutionText = (p: CatalogProposalItem): string => {
  const fr = p.field_resolution as unknown
  if (typeof fr === 'string') return fr && fr !== '{}' ? fr : ''
  if (fr && typeof fr === 'object') {
    return Object.keys(fr).length ? JSON.stringify(fr) : ''
  }
  return ''
}

const rejectOpen = ref(false)
const rejectTarget = ref<CatalogProposalItem | null>(null)
const rejectReason = ref('')

const acting = ref(0)

const act = async (
  p: CatalogProposalItem,
  action: 'approve' | 'reject' | 'withdraw' | 'execute',
  body?: Record<string, unknown>
) => {
  acting.value = p.id
  try {
    const res = await api.post<CatalogProposalActionData>(
      `/admin/catalog/proposals/${p.id}/${action}`,
      body
    )
    if (res.code === 0) {
      const messages: Record<string, string> = {
        approve: `已批准，冷静期至 ${
          res.data?.execute_after
            ? new Date(res.data.execute_after).toLocaleString()
            : '48 小时后'
        }`,
        reject: '已拒绝',
        withdraw: '已撤回',
        execute: '合并已执行'
      }
      useKunMessage(messages[action] ?? '完成', 'success')
      await refresh()
    } else {
      useKunMessage(res.message || '操作失败', 'error')
    }
  } finally {
    acting.value = 0
  }
}

const askReject = (p: CatalogProposalItem) => {
  rejectTarget.value = p
  rejectReason.value = ''
  rejectOpen.value = true
}

const confirmReject = async () => {
  if (!rejectTarget.value) return
  await act(rejectTarget.value, 'reject', { reason: rejectReason.value })
  rejectOpen.value = false
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">目录人审队列</h1>
    <CatalogSubNav />

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
    </div>
    <p class="text-default-500 text-sm">共 {{ total }} 条提案</p>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div class="space-y-2">
      <KunCard v-for="p in items" :key="p.id" content-class="space-y-3 p-4">
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-default-400 text-sm">#{{ p.id }}</span>
          <KunChip color="info" variant="flat" size="xs">
            {{ CATALOG_ENTITY_TYPES[p.entity_type] ?? p.entity_type }}
          </KunChip>
          <span class="text-foreground font-medium">
            {{ p.source.display_name || `#${p.source_entity_id}` }}
          </span>
          <KunIcon name="lucide:arrow-right" class="text-default-400 size-4" />
          <span class="text-foreground font-medium">
            {{ p.target.display_name || `#${p.target_entity_id}` }}
          </span>
          <KunChip
            :color="PROPOSAL_STATUS_COLORS[p.status] ?? 'default'"
            variant="flat"
            size="xs"
            class="ml-auto"
          >
            {{ PROPOSAL_STATUS_LABELS[p.status] ?? p.status }}
          </KunChip>
        </div>

        <p class="text-default-500 text-sm">
          提案人 {{ p.proposed_by }} ·
          {{ new Date(p.proposed_at).toLocaleString() }}
          <template v-if="p.execute_after">
            · 可执行时间 {{ new Date(p.execute_after).toLocaleString() }}
          </template>
        </p>
        <p v-if="p.note" class="text-default-500 text-sm whitespace-pre-line">
          {{ p.note }}
        </p>
        <p
          v-if="fieldResolutionText(p)"
          class="text-default-400 font-mono text-xs"
        >
          field_resolution: {{ fieldResolutionText(p) }}
        </p>

        <div v-if="p.status === PROPOSAL_STATUS.open" class="flex flex-wrap gap-2">
          <KunButton
            color="success"
            size="sm"
            :disabled="acting === p.id"
            @click="act(p, 'approve')"
          >
            批准（进入 48h 冷静期）
          </KunButton>
          <KunButton
            color="danger"
            variant="flat"
            size="sm"
            :disabled="acting === p.id"
            @click="askReject(p)"
          >
            拒绝
          </KunButton>
          <KunButton
            color="default"
            variant="flat"
            size="sm"
            :disabled="acting === p.id"
            @click="act(p, 'withdraw')"
          >
            撤回
          </KunButton>
        </div>
        <div
          v-else-if="p.status === PROPOSAL_STATUS.approved"
          class="flex flex-wrap items-center gap-2"
        >
          <KunButton
            color="warning"
            size="sm"
            :disabled="acting === p.id || coolingRemaining(p) !== null"
            @click="act(p, 'execute')"
          >
            <KunIcon
              v-if="acting === p.id"
              name="lucide:loader-circle"
              class="mr-1 size-4 animate-spin"
            />
            执行合并
          </KunButton>
          <span v-if="coolingRemaining(p)" class="text-warning-600 text-sm">
            冷静期剩余约 {{ coolingRemaining(p) }}（管理员不可豁免）
          </span>
        </div>
      </KunCard>

      <KunCard v-if="!items.length && !error" content-class="p-10">
        <p class="text-default-400 text-center">
          {{ isLoading ? '加载中…' : '没有匹配的提案' }}
        </p>
      </KunCard>
    </div>

    <KunPagination
      v-model:current-page="page"
      :total-page="totalPages"
      :is-loading="isLoading"
    />

    <KunModal v-model="rejectOpen" aria-label="拒绝提案">
      <div class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">拒绝提案</h2>
        <p class="text-default-500 text-sm">
          拒绝提案 #{{ rejectTarget?.id }}，理由将记入提案备注。
        </p>
        <KunInput v-model="rejectReason" placeholder="拒绝理由（可选）" />
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="rejectOpen = false">
            取消
          </KunButton>
          <KunButton color="danger" @click="confirmReject">确认拒绝</KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
