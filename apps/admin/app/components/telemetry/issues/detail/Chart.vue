<script setup lang="ts">
import type { EChartsOption } from 'echarts'
import { telemetryDayList, telemetryWindowRange } from '~/constants/telemetry'
import type { TelemetryIssueDaily } from '~~/shared/types/telemetry'

const props = defineProps<{ daily: TelemetryIssueDaily[] }>()

const range = telemetryWindowRange(30)
const days = telemetryDayList(range.from, range.to)

const eventsByDay = computed(() => {
  const m = new Map<string, number>()
  for (const d of props.daily) m.set(d.day, (m.get(d.day) ?? 0) + d.events)
  return days.map((day) => m.get(day) ?? 0)
})

const colorMode = useColorMode()
const isDark = computed(() => colorMode.value === 'dark')
const palette = computed(() =>
  isDark.value
    ? {
        text: '#9ca3af',
        axis: '#6b7280',
        split: 'rgba(255,255,255,0.06)',
        bar: '#3b82f6',
        tipBg: '#1f2937',
        tipText: '#f3f4f6'
      }
    : {
        text: '#6b7280',
        axis: '#cbd5e1',
        split: 'rgba(0,0,0,0.05)',
        bar: '#2563eb',
        tipBg: '#ffffff',
        tipText: '#111827'
      }
)

const option = computed<EChartsOption>(() => {
  const p = palette.value
  return {
    grid: {
      left: 6,
      right: 14,
      top: 20,
      bottom: 6,
      outerBoundsMode: 'same',
      outerBoundsContain: 'axisLabel'
    },
    tooltip: {
      trigger: 'axis',
      backgroundColor: p.tipBg,
      borderWidth: 0,
      padding: [6, 10],
      textStyle: { color: p.tipText, fontSize: 12 },
      axisPointer: { type: 'shadow' }
    },
    xAxis: {
      type: 'category',
      data: days.map((d) => d.slice(5)),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: p.axis } },
      axisLabel: { color: p.text, fontSize: 11, hideOverlap: true }
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitLine: { lineStyle: { color: p.split } },
      axisLabel: { color: p.text, fontSize: 11 }
    },
    series: [
      {
        type: 'bar',
        name: '事件',
        data: eventsByDay.value,
        barMaxWidth: 26,
        itemStyle: { borderRadius: [4, 4, 0, 0], color: p.bar }
      }
    ]
  }
})
</script>

<template>
  <div class="space-y-2">
    <h2 class="text-foreground text-lg font-semibold">近 30 天事件</h2>
    <KunCard content-class="justify-start items-stretch gap-0" class-name="p-4">
      <ClientOnly>
        <VChart :option="option" :style="{ height: '220px' }" autoresize />
        <template #fallback>
          <div class="bg-default-100 h-[220px] animate-pulse rounded-lg" />
        </template>
      </ClientOnly>
    </KunCard>
  </div>
</template>
