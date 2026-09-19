<script setup lang="ts">
import { jstMonthRange, formatCount } from '~/constants/store'
import type { StoreAdminUsage } from '~~/shared/types/store'

const range = ref<[string, string]>(jstMonthRange(0))

const { data, status, refresh } = await useApiFetch<StoreAdminUsage>(
  '/admin/devapi/store/usage',
  { query: computed(() => ({ from: range.value[0], to: range.value[1] })) }
)

const usage = computed(() => data.value)
const isLoading = computed(() => status.value === 'pending')
const botRatio = computed(() => {
  const u = usage.value
  if (!u || u.total === 0) return '—'
  return `${((u.bots / u.total) * 100).toFixed(1)}%`
})

const cards = computed(() => [
  {
    label: '去重点击',
    value: formatCount(usage.value?.uniques ?? 0),
    hint: '分券按这个数算，不含爬虫',
    icon: 'lucide:mouse-pointer-click',
  },
  {
    label: '总点击',
    value: formatCount(usage.value?.total ?? 0),
    hint: '含重复点击与爬虫',
    icon: 'lucide:activity',
  },
  {
    label: '爬虫点击',
    value: formatCount(usage.value?.bots ?? 0),
    hint: `占总点击 ${botRatio.value}`,
    icon: 'lucide:bot',
  },
  {
    label: '站点 / 短链',
    value: `${usage.value?.by_app.length ?? 0} / ${formatCount(usage.value?.link_count ?? 0)}`,
    hint: '铸过分销短链的应用',
    icon: 'lucide:link',
  },
])
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-foreground">DLsite 分销</h1>
        <p class="mt-1 text-default-500">
          全部下游站点的分销短链点击（JST 日历日），以及按点击占比分发的 DLsite 优惠券
        </p>
      </div>
      <DevapiStoreRange v-model="range" />
    </div>

    <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <KunCard
        v-for="card in cards"
        :key="card.label"
        content-class="justify-start items-stretch gap-1"
        class-name="p-4"
      >
        <div class="flex items-center gap-2 text-sm text-default-500">
          <KunIcon :name="card.icon" class="size-4 text-default-400" />
          {{ card.label }}
        </div>
        <p class="text-2xl font-bold text-foreground">{{ card.value }}</p>
        <p class="text-xs text-default-400">{{ card.hint }}</p>
      </KunCard>
    </div>

    <div v-if="isLoading && !usage" class="flex items-center justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="size-8 animate-spin text-primary" />
    </div>

    <template v-else-if="usage">
      <DevapiStoreTrend :daily="usage.daily" />
      <DevapiStoreApps :apps="usage.by_app" @changed="refresh" />
      <DevapiStoreLinks :links="usage.top_links" />
    </template>

    <DevapiStoreBatches v-if="usage?.can_manage_coupons" :range="range" />
  </div>
</template>
