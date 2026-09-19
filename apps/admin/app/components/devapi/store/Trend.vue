<script setup lang="ts">
import type { EChartsOption } from 'echarts'
import type { StoreUsageDay } from '~~/shared/types/store'

const props = defineProps<{ daily: StoreUsageDay[] }>()

const colorMode = useColorMode()
const isDark = computed(() => colorMode.value === 'dark')
const palette = computed(() =>
  isDark.value
    ? { text: '#9ca3af', axis: '#6b7280', split: 'rgba(255,255,255,0.06)', human: '#3b82f6', repeat: '#1e3a8a', bot: '#6b7280', tipBg: '#1f2937', tipText: '#f3f4f6' }
    : { text: '#6b7280', axis: '#cbd5e1', split: 'rgba(0,0,0,0.05)', human: '#2563eb', repeat: '#93c5fd', bot: '#cbd5e1', tipBg: '#ffffff', tipText: '#111827' }
)

const hasClicks = computed(() => props.daily.some((d) => d.total > 0))

const option = computed<EChartsOption>(() => {
  const p = palette.value
  const bar = (name: string, data: number[], color: string, top = false) => ({
    type: 'bar' as const,
    name,
    stack: 'clicks',
    data,
    barMaxWidth: 26,
    itemStyle: { color, borderRadius: top ? [4, 4, 0, 0] : 0 },
  })
  return {
    grid: { left: 6, right: 14, top: 36, bottom: 6, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
    legend: { top: 0, right: 0, textStyle: { color: p.text, fontSize: 12 }, itemWidth: 12, itemHeight: 8 },
    tooltip: {
      trigger: 'axis',
      backgroundColor: p.tipBg,
      borderWidth: 0,
      padding: [6, 10],
      textStyle: { color: p.tipText, fontSize: 12 },
      axisPointer: { type: 'shadow' },
    },
    xAxis: {
      type: 'category',
      data: props.daily.map((d) => d.day.slice(5)),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: p.axis } },
      axisLabel: { color: p.text, fontSize: 11, hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: p.split } },
      axisLabel: { color: p.text, fontSize: 11 },
    },
    series: [
      bar('去重点击', props.daily.map((d) => d.uniques), p.human),
      bar('重复点击', props.daily.map((d) => Math.max(d.total - d.uniques - d.bots, 0)), p.repeat),
      bar('爬虫', props.daily.map((d) => d.bots), p.bot, true),
    ],
  }
})
</script>

<template>
  <div class="space-y-2">
    <h2 class="text-lg font-semibold text-foreground">每日点击</h2>
    <KunCard content-class="justify-start items-stretch gap-0" class-name="p-4">
      <ClientOnly>
        <VChart :option="option" :style="{ height: '280px' }" autoresize />
        <template #fallback>
          <div class="h-[280px] animate-pulse rounded-lg bg-default-100" />
        </template>
      </ClientOnly>
      <p v-if="!hasClicks" class="mt-2 text-center text-xs text-default-400">
        这个区间没有点击
      </p>
    </KunCard>
  </div>
</template>
