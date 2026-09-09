<script setup lang="ts">
import {
  moemoepointReasonLabel,
  moemoepointSourceLabel
} from '~/constants/moemoepoint'

const api = useApi()

const PAGE_SIZE = 20

const items = ref<MoemoepointLogItem[]>([])
const hasMore = ref(false)
const loading = ref(false)
const loaded = ref(false) // first page settled — drives the empty state

const loadPage = async () => {
  if (loading.value) return
  loading.value = true
  try {
    const beforeId = items.value.length
      ? items.value[items.value.length - 1]!.id
      : undefined
    const res = await api.get<{ items: MoemoepointLogItem[]; has_more: boolean }>(
      '/auth/me/moemoepoint/log',
      { limit: PAGE_SIZE, before_id: beforeId }
    )
    if (res.code === 0 && res.data) {
      items.value.push(...(res.data.items ?? []))
      hasMore.value = res.data.has_more
    }
  } finally {
    loading.value = false
    loaded.value = true
  }
}

onMounted(loadPage)

const formatDate = (s: string) =>
  new Date(s).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
</script>

<template>
  <KunCard class="p-6">
    <div class="mb-4 flex items-center justify-between">
      <h2 class="text-lg font-semibold text-foreground">萌萌点记录</h2>
      <span class="text-default-400 text-sm">仅你可见</span>
    </div>

    <div v-if="!loaded && loading" class="flex justify-center py-8">
      <KunIcon name="lucide:loader-circle" class="size-6 animate-spin text-primary" />
    </div>

    <div
      v-else-if="items.length === 0"
      class="text-default-400 py-8 text-center text-sm"
    >
      暂无萌萌点变动记录
    </div>

    <ul v-else class="space-y-2">
      <li
        v-for="row in items"
        :key="row.id"
        class="flex items-center justify-between gap-3 rounded-lg border border-default-200 px-3 py-2"
      >
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span class="text-sm text-foreground">
              {{ moemoepointReasonLabel(row.reason, row.ref) }}
            </span>
            <span v-if="row.source_app" class="text-default-400 text-xs">
              来自 {{ row.source_name || moemoepointSourceLabel(row.source_app) }}
            </span>
          </div>
          <p v-if="row.ref" class="text-default-400 truncate text-xs">
            {{ row.ref }}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-3">
          <span
            :class="row.delta >= 0 ? 'text-success' : 'text-danger'"
            class="font-mono text-sm font-semibold"
          >
            {{ row.delta >= 0 ? '+' : '' }}{{ row.delta }}
          </span>
          <span class="text-default-400 text-xs">{{ formatDate(row.created_at) }}</span>
        </div>
      </li>
    </ul>

    <div v-if="hasMore" class="mt-4 flex justify-center">
      <KunButton
        variant="flat"
        color="default"
        :disabled="loading"
        @click="loadPage"
      >
        <KunIcon
          v-if="loading"
          name="lucide:loader-circle"
          class="mr-1 size-4 animate-spin"
        />
        加载更多
      </KunButton>
    </div>
  </KunCard>
</template>
