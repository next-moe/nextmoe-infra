<script setup lang="ts">
import type { EChartsOption } from 'echarts'
import { telemetryDayList } from '~/constants/telemetry'
import type { TelemetryDailyMetric } from '~~/shared/types/telemetry'

const props = defineProps<{
  rows: TelemetryDailyMetric[]
  range: { from: string; to: string }
}>()

const days = computed(() => telemetryDayList(props.range.from, props.range.to))

const series = computed(() => {
  const sessions = new Map<string, number>()
  const crashed = new Map<string, number>()
  const anr = new Map<string, number>()
  for (const r of props.rows) {
    sessions.set(r.day, (sessions.get(r.day) ?? 0) + r.sessions)
    crashed.set(r.day, (crashed.get(r.day) ?? 0) + r.crashed_sessions)
    anr.set(r.day, (anr.get(r.day) ?? 0) + r.anr_sessions)
  }
  return {
    sessions: days.value.map((d) => sessions.get(d) ?? 0),
    crash: days.value.map((d) => {
      const s = sessions.get(d) ?? 0
      if (s <= 0) return null
      return ((crashed.get(d) ?? 0) / s) * 100
    }),
    anr: days.value.map((d) => {
      const s = sessions.get(d) ?? 0
      if (s <= 0) return null
      return ((anr.get(d) ?? 0) / s) * 100
    })
  }
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
        crash: '#f87171',
        anr: '#fbbf24',
        tipBg: '#1f2937',
        tipText: '#f3f4f6'
      }
    : {
        text: '#6b7280',
        axis: '#cbd5e1',
        split: 'rgba(0,0,0,0.05)',
        bar: '#2563eb',
        crash: '#dc2626',
        anr: '#d97706',
        tipBg: '#ffffff',
        tipText: '#111827'
      }
)

const option = computed<EChartsOption>(() => {
  const p = palette.value
  return {
    grid: {
      left: 6,
      right: 44,
      top: 28,
      bottom: 6,
      outerBoundsMode: 'same',
      outerBoundsContain: 'axisLabel'
    },
    legend: {
      top: 0,
      right: 0,
      textStyle: { color: p.text, fontSize: 12 },
      itemWidth: 12,
      itemHeight: 8
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
      data: days.value.map((d) => d.slice(5)),
      axisTick: { show: false },
      axisLine: { lineStyle: { color: p.axis } },
      axisLabel: { color: p.text, fontSize: 11, hideOverlap: true }
    },
    yAxis: [
      {
        type: 'value',
        minInterval: 1,
        splitLine: { lineStyle: { color: p.split } },
        axisLabel: { color: p.text, fontSize: 11 }
      },
      {
        type: 'value',
        min: 0,
        axisLabel: {
          color: p.text,
          fontSize: 11,
          formatter: (v: number) => `${v}%`
        },
        splitLine: { show: false }
      }
    ],
    series: [
      {
        type: 'bar',
        name: '会话数',
        data: series.value.sessions,
        barMaxWidth: 26,
        itemStyle: { borderRadius: [4, 4, 0, 0], color: p.bar },
        tooltip: {
          valueFormatter: (v) =>
            v == null ? '—' : Number(v).toLocaleString('zh-CN')
        }
      },
      {
        type: 'line',
        name: '崩溃率',
        yAxisIndex: 1,
        data: series.value.crash,
        showSymbol: false,
        itemStyle: { color: p.crash },
        lineStyle: { width: 2 },
        tooltip: {
          valueFormatter: (v) => (v == null ? '—' : `${Number(v).toFixed(2)}%`)
        }
      },
      {
        type: 'line',
        name: 'ANR 率',
        yAxisIndex: 1,
        data: series.value.anr,
        showSymbol: false,
        itemStyle: { color: p.anr },
        lineStyle: { width: 2 },
        tooltip: {
          valueFormatter: (v) => (v == null ? '—' : `${Number(v).toFixed(2)}%`)
        }
      }
    ]
  }
})
</script>

<template>
  <div class="space-y-2">
    <h2 class="text-foreground text-lg font-semibold">每日趋势</h2>
    <KunCard content-class="justify-start items-stretch gap-0" class-name="p-4">
      <ClientOnly>
        <VChart :option="option" :style="{ height: '260px' }" autoresize />
        <template #fallback>
          <div class="bg-default-100 h-[260px] animate-pulse rounded-lg" />
        </template>
      </ClientOnly>
    </KunCard>
  </div>
</template>
