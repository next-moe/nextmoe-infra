<script setup lang="ts">
import { REGISTRATION_RANGE_OPTIONS } from '~/constants/stats'
import type { EChartsOption } from 'echarts'

const days = ref(30)
const { data: daily, status: dailyStatus } = await useApiFetch<RegistrationStats>(
  '/admin/stats/registrations',
  { query: computed(() => ({ days: days.value })) }
)

const latestDate = computed(() => daily.value?.series.at(-1)?.date ?? '')
const selectedDate = ref(daily.value?.series.at(-1)?.date ?? '')
const { data: hourly } = await useApiFetch<HourlyStats>(
  '/admin/stats/registrations/hourly',
  { query: computed(() => ({ date: selectedDate.value })) }
)

const summary = computed(() => daily.value?.summary)
const fmtPct = (n: number) => `${n > 0 ? '+' : ''}${n}%`

const colorMode = useColorMode()
const isDark = computed(() => colorMode.value === 'dark')
const palette = computed(() =>
  isDark.value
    ? { text: '#9ca3af', axis: '#6b7280', split: 'rgba(255,255,255,0.06)', dFrom: '#3b82f6', dTo: '#1d4ed8', hFrom: '#22d3ee', hTo: '#0891b2', tipBg: '#1f2937', tipText: '#f3f4f6' }
    : { text: '#6b7280', axis: '#cbd5e1', split: 'rgba(0,0,0,0.05)', dFrom: '#60a5fa', dTo: '#2563eb', hFrom: '#67e8f9', hTo: '#0e7490', tipBg: '#ffffff', tipText: '#111827' }
)
const grad = (from: string, to: string) => ({
  type: 'linear' as const, x: 0, y: 0, x2: 0, y2: 1,
  colorStops: [{ offset: 0, color: from }, { offset: 1, color: to }],
})

const dailyOption = computed<EChartsOption>(() => {
  const s = daily.value?.series ?? []
  const p = palette.value
  const avg = daily.value?.summary.daily_average ?? 0
  return {
    grid: { left: 6, right: 14, top: 20, bottom: 6, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
    tooltip: { trigger: 'axis', backgroundColor: p.tipBg, borderWidth: 0, padding: [6, 10], textStyle: { color: p.tipText, fontSize: 12 }, axisPointer: { type: 'shadow' } },
    xAxis: { type: 'category', data: s.map((d) => d.date.slice(5)), axisTick: { show: false }, axisLine: { lineStyle: { color: p.axis } }, axisLabel: { color: p.text, fontSize: 11, hideOverlap: true } },
    yAxis: { type: 'value', minInterval: 1, splitLine: { lineStyle: { color: p.split } }, axisLabel: { color: p.text, fontSize: 11 } },
    series: [
      {
        type: 'bar', name: '注册', data: s.map((d) => d.count), barMaxWidth: 26, cursor: 'pointer',
        itemStyle: { borderRadius: [4, 4, 0, 0], color: grad(p.dFrom, p.dTo) },
        emphasis: { itemStyle: { color: grad(p.dTo, p.dFrom) } },
        markLine: s.length ? { silent: true, symbol: 'none', lineStyle: { color: p.axis, type: 'dashed', width: 1 }, label: { color: p.text, fontSize: 11, formatter: `日均 ${avg}` }, data: [{ yAxis: avg }] } : undefined,
      },
    ],
  }
})

const hourLabels = Array.from({ length: 24 }, (_, h) => String(h).padStart(2, '0'))
const hourlyOption = computed<EChartsOption>(() => {
  const s = hourly.value?.series ?? []
  const p = palette.value
  return {
    grid: { left: 6, right: 14, top: 16, bottom: 6, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
    tooltip: { trigger: 'axis', backgroundColor: p.tipBg, borderWidth: 0, padding: [6, 10], textStyle: { color: p.tipText, fontSize: 12 }, axisPointer: { type: 'shadow' }, formatter: (ps: unknown) => { const x = (ps as { axisValue: string; data: number }[])[0]!; return `${x.axisValue}:00<br/>注册 ${x.data}` } },
    xAxis: { type: 'category', data: hourLabels, axisTick: { show: false }, axisLine: { lineStyle: { color: p.axis } }, axisLabel: { color: p.text, fontSize: 11, interval: 1 } },
    yAxis: { type: 'value', minInterval: 1, splitLine: { lineStyle: { color: p.split } }, axisLabel: { color: p.text, fontSize: 11 } },
    series: [{ type: 'bar', name: '注册', data: s.map((d) => d.count), barMaxWidth: 22, itemStyle: { borderRadius: [3, 3, 0, 0], color: grad(p.hFrom, p.hTo) } }],
  }
})

const onDailyClick = (params: { dataIndex: number }) => {
  const d = daily.value?.series[params.dataIndex]
  if (d) selectedDate.value = d.date
}
</script>

<template>
  <KunCard content-class="justify-start items-stretch gap-0" class-name="p-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-lg font-semibold text-foreground">用户注册统计</h2>
      <div class="flex flex-wrap gap-2">
        <KunButton
          v-for="opt in REGISTRATION_RANGE_OPTIONS"
          :key="opt.value"
          size="sm"
          :variant="days === opt.value ? 'flat' : 'light'"
          :color="days === opt.value ? 'primary' : 'default'"
          @click="days = opt.value"
        >
          {{ opt.label }}
        </KunButton>
      </div>
    </div>

    <div v-if="summary" class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
      <div class="border-default-200 rounded-lg border p-3">
        <p class="text-default-500 text-xs">{{ days }} 天注册</p>
        <p class="text-foreground mt-1 text-xl font-bold tabular-nums">{{ summary.total }}</p>
        <p class="mt-0.5 text-xs" :class="summary.growth_percent >= 0 ? 'text-success-600' : 'text-danger-500'">
          {{ fmtPct(summary.growth_percent) }} 较上一周期
        </p>
      </div>
      <div class="border-default-200 rounded-lg border p-3">
        <p class="text-default-500 text-xs">日均</p>
        <p class="text-foreground mt-1 text-xl font-bold tabular-nums">{{ summary.daily_average }}</p>
        <p class="text-default-400 mt-0.5 text-xs">每天平均</p>
      </div>
      <div class="border-default-200 rounded-lg border p-3">
        <p class="text-default-500 text-xs">单日峰值</p>
        <p class="text-foreground mt-1 text-xl font-bold tabular-nums">{{ summary.peak_count }}</p>
        <p class="text-default-400 mt-0.5 text-xs">{{ summary.peak_date?.slice(5) || '—' }}</p>
      </div>
      <div class="border-default-200 rounded-lg border p-3">
        <p class="text-default-500 text-xs">今日</p>
        <p class="text-foreground mt-1 text-xl font-bold tabular-nums">{{ summary.today }}</p>
        <p class="text-default-400 mt-0.5 text-xs">总用户 {{ summary.total_all_time }}</p>
      </div>
    </div>

    <ClientOnly>
      <VChart
        :option="dailyOption"
        :style="{ height: '300px' }"
        autoresize
        class="mt-4"
        @click="onDailyClick"
      />
      <template #fallback>
        <div class="bg-default-100 mt-4 h-[300px] animate-pulse rounded-lg" />
      </template>
    </ClientOnly>
    <p class="text-default-400 mt-1 text-center text-xs">
      点击柱状图任意一天，查看该天 24 小时注册分布 · 时区 {{ daily?.timezone || 'Asia/Shanghai' }}
    </p>

    <KunDivider class="my-5" />

    <div class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="text-foreground text-base font-semibold">单日小时分布</h3>
      <div class="flex items-center gap-2">
        <input
          v-model="selectedDate"
          type="date"
          aria-label="选择要查看小时分布的日期"
          :max="latestDate"
          class="border-default-200 bg-background text-foreground rounded-lg border px-2 py-1 text-sm"
        >
        <span class="text-default-500 text-sm whitespace-nowrap">当日共 {{ hourly?.total ?? 0 }}</span>
      </div>
    </div>
    <ClientOnly>
      <VChart :option="hourlyOption" :style="{ height: '220px' }" autoresize class="mt-3" />
      <template #fallback>
        <div class="bg-default-100 mt-3 h-[220px] animate-pulse rounded-lg" />
      </template>
    </ClientOnly>

    <p v-if="dailyStatus === 'pending'" class="text-default-400 mt-2 text-center text-xs">加载中…</p>
  </KunCard>
</template>
