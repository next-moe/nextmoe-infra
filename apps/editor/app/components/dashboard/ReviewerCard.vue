<script setup lang="ts">
import type { ActivityList } from '#shared/types/activity'
import type { CatalogProposal } from '#shared/types/catalog'

const api = useEditorApi()

const { data, pending } = useLazyAsyncData('dashboard-reviewer-queue', () =>
  api<ActivityList<CatalogProposal>>('/v2/moderation/proposals', {
    query: { limit: 1, include_total: true, sort: 'filed_asc' }
  }).catch(() => null)
)

const total = computed(
  () => data.value?.total ?? data.value?.items?.length ?? 0
)
const oldest = computed(() => data.value?.items?.[0] ?? null)
</script>

<template>
  <KunCard bordered padding="md">
    <div class="mb-3 flex items-center justify-between">
      <h2 class="font-semibold">审核队列</h2>
      <NuxtLink to="/review" class="text-primary text-sm">去审核</NuxtLink>
    </div>
    <div v-if="pending && !data" class="grid grid-cols-2 gap-3">
      <div v-for="index in 2" :key="index" class="bg-default-50 rounded-lg p-3">
        <KunSkeleton variant="text" class-name="text-2xl" width="3rem" />
        <KunSkeleton variant="text" class-name="text-sm" width="4rem" />
      </div>
    </div>
    <div v-else class="grid grid-cols-2 gap-3">
      <div class="bg-default-50 rounded-lg p-3">
        <p class="text-2xl font-semibold tabular-nums">{{ total }}</p>
        <p class="text-default-500 text-sm">等待审核</p>
      </div>
      <div class="bg-default-50 rounded-lg p-3">
        <p class="text-lg font-semibold">
          {{ oldest ? formatRelative(oldest.created_at) : '—' }}
        </p>
        <p class="text-default-500 text-sm">最早等待</p>
      </div>
    </div>
  </KunCard>
</template>
