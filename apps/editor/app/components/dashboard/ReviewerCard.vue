<script setup lang="ts">
import type { ActivityList } from '#shared/types/activity'
import type { CatalogProposal } from '#shared/types/catalog'

const PAGE = 100

const api = useEditorApi()

const { data, pending } = await useAsyncData('dashboard-reviewer-queue', () =>
  api<ActivityList<CatalogProposal>>('/v2/moderation/proposals', {
    query: { limit: PAGE, include_total: true }
  }).catch(() => null)
)

const total = computed(
  () => data.value?.total ?? data.value?.items?.length ?? 0
)
const oldest = computed(() => data.value?.items?.at(-1) ?? null)
const oldestIsPartial = computed(
  () => (data.value?.items?.length ?? 0) < total.value
)
</script>

<template>
  <KunCard bordered padding="md">
    <div class="mb-3 flex items-center justify-between">
      <h2 class="font-semibold">审核队列</h2>
      <NuxtLink to="/review" class="text-primary text-sm">去审核</NuxtLink>
    </div>
    <KunLoading v-if="pending" spinner />
    <div v-else class="grid grid-cols-2 gap-3">
      <div class="bg-default-50 rounded-lg p-3">
        <p class="text-2xl font-semibold tabular-nums">{{ total }}</p>
        <p class="text-default-500 text-sm">等待审核</p>
      </div>
      <div class="bg-default-50 rounded-lg p-3">
        <p class="text-lg font-semibold">
          {{ oldest ? formatRelative(oldest.created_at) : '—' }}
        </p>
        <p class="text-default-500 text-sm">
          {{ oldestIsPartial ? `最早等待（前 ${PAGE} 条）` : '最早等待' }}
        </p>
      </div>
    </div>
  </KunCard>
</template>
