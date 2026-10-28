<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import {
  LLM_API,
  LLM_CATALOG_IMAGE_PROBE_COST,
  LLM_CATALOG_KIND_META,
  LLM_CATALOG_KINDS,
  LLM_CATALOG_STATUS_TABS
} from '~/constants/llm'
import type {
  LlmCatalogEntry,
  LlmCatalogKind,
  LlmCatalogSyncResult,
  LlmProvider
} from '~~/shared/types/llm'

const props = defineProps<{ provider: LlmProvider | null }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ adopted: [] }>()

const api = useApi('ai')

const entries = ref<LlmCatalogEntry[]>([])
const loading = ref(false)
const failed = ref('')
const syncing = ref(false)
const syncError = ref('')
const orphaned = ref<number[]>([])
const busy = ref('')

const status = ref('listed')
const kind = ref<LlmCatalogKind | 'all'>('all')
const keyword = ref('')

const adoptOpen = ref(false)
const adopting = ref<LlmCatalogEntry | null>(null)

const load = async () => {
  if (!props.provider) return
  failed.value = ''
  const res = await api.get<LlmCatalogEntry[]>(
    `${LLM_API}/providers/${props.provider.id}/models`
  )
  if (res.code === 0) entries.value = res.data ?? []
  else failed.value = res.message || '加载失败'
}

watch(open, async (v) => {
  if (!v) return
  entries.value = []
  syncError.value = ''
  orphaned.value = []
  status.value = 'listed'
  kind.value = 'all'
  keyword.value = ''
  loading.value = true
  await load()
  loading.value = false
})

const lastSeen = computed(() =>
  entries.value.reduce<string | null>(
    (max, e) => (!max || e.last_seen_at > max ? e.last_seen_at : max),
    null
  )
)
const listedCount = computed(() => entries.value.filter((e) => e.listed).length)

const statusTabs = computed<KunTabItem[]>(() =>
  LLM_CATALOG_STATUS_TABS.map((t) => {
    const n =
      t.value === 'all'
        ? entries.value.length
        : t.value === 'listed'
          ? listedCount.value
          : entries.value.length - listedCount.value
    return { value: t.value, textValue: `${t.label} ${n}` }
  })
)

const byStatus = computed(() =>
  status.value === 'all'
    ? entries.value
    : entries.value.filter((e) => e.listed === (status.value === 'listed'))
)

const kindOptions = computed(() => [
  { value: 'all' as const, label: `全部类型 (${byStatus.value.length})` },
  ...LLM_CATALOG_KINDS.map((k) => ({
    value: k,
    label: `${LLM_CATALOG_KIND_META[k].label} (${
      byStatus.value.filter((e) => e.kind === k).length
    })`
  }))
])

const visible = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return byStatus.value.filter(
    (e) =>
      (kind.value === 'all' || e.kind === kind.value) &&
      (!q || e.upstream_id.toLowerCase().includes(q))
  )
})

const replace = (next: LlmCatalogEntry) => {
  entries.value = entries.value.map((e) => (e.id === next.id ? next : e))
}

const syncSummary = (r: LlmCatalogSyncResult) =>
  [
    `共 ${r.listed} 个`,
    `新增 ${r.added}`,
    `下架 ${r.delisted}`,
    ...(r.relisted ? [`重新上架 ${r.relisted}`] : [])
  ].join(',')

const sync = async () => {
  if (!props.provider) return
  syncing.value = true
  syncError.value = ''
  try {
    const res = await api.post<LlmCatalogSyncResult>(
      `${LLM_API}/providers/${props.provider.id}/models/sync`
    )
    if (res.code !== 0) {
      syncError.value = res.message || '同步失败'
      useKunMessage('同步失败', 'error')
      return
    }
    orphaned.value = res.data.orphaned_routes
    useKunMessage(
      syncSummary(res.data),
      orphaned.value.length ? 'warn' : 'success'
    )
    await load()
  } finally {
    syncing.value = false
  }
}

const setKind = async (e: LlmCatalogEntry, next: LlmCatalogKind) => {
  busy.value = `${e.id}:kind`
  try {
    const res = await api.patch<LlmCatalogEntry>(
      `${LLM_API}/provider-models/${e.id}`,
      { kind: next }
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '更新失败', 'error')
      return
    }
    replace(res.data)
    useKunMessage(`已改为「${LLM_CATALOG_KIND_META[next].label}」`, 'success')
  } finally {
    busy.value = ''
  }
}

const probe = async (e: LlmCatalogEntry) => {
  if (e.kind === 'image') {
    const confirmed = await useKunAlert({
      title: `测试「${e.upstream_id}」?`,
      message: `图片模型的测试会真实生成一张图,按次计费(${LLM_CATALOG_IMAGE_PROBE_COST})。`,
      type: 'warning',
      confirmText: '生成并测试'
    })
    if (!confirmed) return
  }
  busy.value = `${e.id}:probe`
  try {
    const res = await api.post<LlmCatalogEntry>(
      `${LLM_API}/provider-models/${e.id}/probe`,
      {}
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '测试请求失败', 'error')
      return
    }
    replace(res.data)
    const p = res.data.probe
    if (p?.ok) useKunMessage(`测试通过 · ${p.latency_ms} ms`, 'success')
    else
      useKunMessage(
        `测试失败${p?.error_kind ? `:${p.error_kind}` : ''}`,
        'error'
      )
  } finally {
    busy.value = ''
  }
}

const openAdopt = (e: LlmCatalogEntry) => {
  adopting.value = e
  adoptOpen.value = true
}

const onAdopted = async () => {
  emit('adopted')
  await load()
}
</script>

<template>
  <KunDrawer
    v-model="open"
    placement="right"
    size="xl"
    :title="provider ? `${provider.name} · 可用模型` : '可用模型'"
    inner-class-name="w-[56rem] max-w-full"
  >
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-default-500 text-sm">
          <template v-if="entries.length">
            上次同步 {{ llmTime(lastSeen) }} · 在列 {{ listedCount }} / 共
            {{ entries.length }}
          </template>
          <template v-else-if="!loading">尚未同步</template>
        </p>
        <KunButton
          color="primary"
          size="sm"
          :loading="syncing"
          :disabled="loading"
          @click="sync"
        >
          <KunIcon name="lucide:refresh-cw" class="mr-2 size-4" />
          同步模型列表
        </KunButton>
      </div>

      <div
        v-if="syncError"
        class="border-danger-200 bg-danger-50 flex items-start gap-3 rounded-xl border p-4"
      >
        <KunIcon
          name="lucide:circle-alert"
          class="text-danger size-5 shrink-0"
        />
        <div class="text-danger-700 min-w-0 space-y-1 text-sm">
          <p class="font-medium">无法同步该供应商的模型列表</p>
          <p class="font-mono text-xs break-all">{{ syncError }}</p>
          <p class="text-xs">
            供应商不提供 /models 列表时,可以在「路由」里手动填写上游模型 ID。
          </p>
        </div>
      </div>

      <div
        v-if="orphaned.length"
        class="border-warning-200 bg-warning-50 flex items-start gap-3 rounded-xl border p-4"
      >
        <KunIcon
          name="lucide:triangle-alert"
          class="text-warning-600 size-5 shrink-0"
        />
        <p class="text-warning-700 text-sm">
          路由
          <span class="font-mono">
            {{ orphaned.map((id) => `#${id}`).join('、') }}
          </span>
          指向的上游模型已不在供应商的列表里,调用可能会失败;请检查或停用这些路由。
        </p>
      </div>

      <CommonFetchError v-if="failed" :message="failed" @retry="load" />

      <div v-if="loading" class="flex justify-center py-12">
        <KunIcon
          name="lucide:loader-circle"
          class="text-primary size-8 animate-spin"
        />
      </div>

      <template v-else-if="entries.length">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
          <KunTab
            v-model="status"
            :items="statusTabs"
            variant="pills"
            size="sm"
            aria-label="上架状态"
          />
          <div class="grid flex-1 grid-cols-1 gap-3 sm:grid-cols-2">
            <KunSelect
              v-model="kind"
              :options="kindOptions"
              aria-label="按类型筛选"
              size="sm"
            />
            <KunInput
              v-model="keyword"
              placeholder="搜索上游模型 ID"
              aria-label="搜索上游模型 ID"
              size="sm"
            >
              <template #prefix>
                <KunIcon name="lucide:search" class="text-default-400 size-4" />
              </template>
            </KunInput>
          </div>
        </div>

        <div
          class="border-default-200 divide-default-200 divide-y overflow-hidden rounded-xl border"
        >
          <AiLlmCatalogEntry
            v-for="e in visible"
            :key="e.id"
            :entry="e"
            :busy="busy"
            @kind="setKind(e, $event)"
            @probe="probe(e)"
            @adopt="openAdopt(e)"
          />
          <p
            v-if="!visible.length"
            class="text-default-400 px-4 py-8 text-center text-sm"
          >
            没有符合条件的模型
          </p>
        </div>
      </template>

      <KunCard v-else-if="!failed" class-name="py-12 text-center">
        <p class="text-default-400">
          还没有模型列表;点「同步模型列表」从供应商的 /models 拉取
        </p>
      </KunCard>
    </div>

    <AiLlmAdoptModal
      v-model="adoptOpen"
      :entry="adopting"
      @adopted="onAdopted"
    />
  </KunDrawer>
</template>
