<script setup lang="ts">
import { STORE_SCOPE, STORE_USAGE_DAY_OPTIONS } from '~/constants/store'
import type { StoreUsageSummary } from '~~/shared/types/store'

useSeoMeta({ title: '分销链接', robots: 'noindex' })

const days = ref(30)

const { data, status } = await useApiFetch<StoreUsageSummary>(
  () => `/dev/store/usage?days=${days.value}`
)

const summary = computed(() => data.value)
const daily = computed(() => summary.value?.daily ?? [])
const byApp = computed(() => summary.value?.by_app ?? [])
const byLink = computed(() => summary.value?.by_link ?? [])
const linkCount = computed(() => summary.value?.link_count ?? 0)
const total = computed(() => summary.value?.total ?? 0)
const uniques = computed(() => summary.value?.uniques ?? 0)

const onboarded = computed(() => linkCount.value > 0)

const fmt = (n: number) => n.toLocaleString()

const dedupPct = computed(() =>
  total.value > 0 ? Math.round((uniques.value / total.value) * 100) : 0
)

const peak = computed(() =>
  daily.value.reduce((best, d) => (d.uniques > best.uniques ? d : best), {
    day: '',
    uniques: 0,
    total: 0
  })
)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 class="text-foreground text-2xl font-bold">分销链接</h1>
        <p class="text-default-500 mt-1 text-sm">
          你名下应用的 DLsite 分销短链点击量,按 JST 日统计。
        </p>
      </div>
      <div
        v-if="onboarded"
        class="border-default-200 flex rounded-lg border p-0.5"
      >
        <button
          v-for="opt in STORE_USAGE_DAY_OPTIONS"
          :key="opt"
          type="button"
          class="rounded-md px-3 py-1 text-sm font-medium transition-colors"
          :class="
            days === opt
              ? 'bg-primary-50 text-primary'
              : 'text-default-500 hover:text-foreground'
          "
          @click="days = opt"
        >
          {{ opt }} 天
        </button>
      </div>
    </div>

    <KunCard
      v-if="!onboarded"
      content-class="items-start gap-4"
      class-name="p-8"
    >
      <div class="flex items-center gap-3">
        <KunIcon name="lucide:shopping-bag" class="text-primary size-5" />
        <h2 class="text-foreground text-lg font-semibold">
          还没有接入分销链接
        </h2>
      </div>
      <p class="text-default-500 max-w-2xl text-sm leading-relaxed">
        分销链接 API 会给你的站点单独签发一条 DLsite
        短链:同一个商品,每个站拿到的链接都不一样,
        点击才归得到你这里。活动期还会附带优惠券领取链接。
      </p>
      <p class="text-default-500 max-w-2xl text-sm leading-relaxed">
        先在控制台铸密钥时勾选
        <code class="bg-default-100 rounded px-1.5 py-0.5 font-mono text-xs">
          {{ STORE_SCOPE }}
        </code>
        ,然后调一次
        <code class="bg-default-100 rounded px-1.5 py-0.5 font-mono text-xs">
          GET /v2/store/purchase-links/{product_id}
        </code>
        就会铸出你的第一条链接,数据随后出现在这里。
      </p>
      <div class="flex flex-wrap gap-3">
        <KunButton color="primary" size="md" @click="navigateTo('/dashboard')">
          去控制台铸密钥
          <KunIcon name="lucide:arrow-right" class="ml-1 size-4" />
        </KunButton>
        <KunButton variant="flat" size="md" @click="navigateTo('/docs/v2')">
          <KunIcon name="lucide:book-open" class="mr-1 size-4" />
          阅读接入文档
        </KunButton>
      </div>
    </KunCard>

    <template v-else>
      <DashboardStoreCoupons />

      <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
        <ChartStatTile
          label="去重点击"
          :value="uniques"
          :spark="daily.map((d) => d.uniques)"
          :hint="`最近 ${days} 天`"
        />
        <ChartStatTile
          label="总点击"
          :value="total"
          :spark="daily.map((d) => d.total)"
          :hint="`含重复与爬虫 ${fmt(total - uniques)}`"
        />
        <ChartStatTile
          label="去重占比"
          :value="`${dedupPct}%`"
          :meter="dedupPct"
          :hint="`每 100 次点击计入 ${dedupPct} 次`"
        />
        <ChartStatTile
          label="单日峰值"
          :value="peak.uniques"
          tone="foreground"
          :hint="peak.day ? peak.day : '—'"
        />
      </div>

      <KunCard
        content-class="justify-start gap-0 items-stretch"
        class-name="p-6"
      >
        <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2">
          <h2 class="text-foreground text-base font-semibold">每日点击</h2>
          <span class="text-default-400 text-xs">
            最近 {{ days }} 天 · JST · 已铸链接 {{ fmt(linkCount) }} 条
          </span>
        </div>
        <DashboardStoreTrend :days="daily" />
      </KunCard>

      <KunCard
        v-if="days >= 14"
        content-class="justify-start gap-0 items-stretch"
        class-name="p-6"
      >
        <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2">
          <h2 class="text-foreground text-base font-semibold">星期分布</h2>
          <span class="text-default-400 text-xs">每个星期几的日均去重点击</span>
        </div>
        <DashboardStoreWeekday :days="daily" />
      </KunCard>

      <div>
        <h2 class="text-foreground mb-3 text-lg font-semibold">按应用</h2>
        <KunCard
          content-class="justify-start gap-0 items-stretch"
          class-name="p-6"
        >
          <DashboardStoreApps :rows="byApp" />
        </KunCard>
      </div>

      <div>
        <h2 class="text-foreground mb-3 text-lg font-semibold">按链接</h2>
        <DashboardStoreLinks :rows="byLink" />
      </div>

      <p class="text-default-400 text-xs leading-relaxed">
        去重口径:同一条短链、同一个 JST 日、同一个指纹(访问 IP 与 User-Agent 的
        SHA-256)只算一次——这是我们对 DLsite
        的承诺;User-Agent 自报是爬虫、链接预览或 HTTP 库的访问照常跳转,但不计入去重点击。计数每小时从跳转器同步一次,当天数字始终是不完整的。
      </p>
    </template>

    <p v-if="status === 'pending'" class="text-default-400 text-center text-xs">
      加载中…
    </p>
  </div>
</template>
