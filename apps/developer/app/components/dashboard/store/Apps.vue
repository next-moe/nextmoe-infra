<script setup lang="ts">
import type { EChartsOption } from 'echarts'
import { CHART_BAR_CATEGORY_GAP, CHART_STACK_GAP } from '~/constants/chart'
import type { StoreUsageApp } from '~~/shared/types/store'

const props = defineProps<{ rows: StoreUsageApp[] }>()

const theme = useChartTheme()

const fmt = (n: number) => n.toLocaleString()

// ECharts stacks a horizontal bar bottom-up, so the ranked list has to be
// reversed for the biggest app to land at the top of the plot.
const plotted = computed(() => [...props.rows].reverse())

const option = computed<EChartsOption>(() => {
  const t = theme.value
  const border = { borderColor: t.surface, borderWidth: CHART_STACK_GAP }
  return {
    ...chartBase(t),
    legend: {
      data: ['去重点击', '重复与爬虫'],
      top: 0,
      left: 0,
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      itemGap: 18,
      textStyle: { color: t.ink, fontSize: 12 }
    },
    grid: chartGrid({ top: 36, right: 16 }),
    tooltip: {
      ...(chartBase(t).tooltip as object),
      formatter: (params: unknown) => {
        const rows = params as { dataIndex: number }[]
        const row = plotted.value[rows[0]!.dataIndex]
        if (!row) return ''
        return [
          row.name,
          `去重 <b>${fmt(row.uniques)}</b>`,
          `重复与爬虫 ${fmt(row.total - row.uniques)}`,
          `链接 ${fmt(row.links)}`
        ].join('<br/>')
      }
    },
    xAxis: { ...valueAxis(t), splitLine: { lineStyle: { color: t.split } } },
    yAxis: {
      type: 'category',
      data: plotted.value.map((r) => r.name),
      axisTick: { show: false },
      axisLine: { show: false },
      axisLabel: {
        color: t.ink,
        fontSize: 12,
        width: 120,
        overflow: 'truncate'
      }
    },
    series: [
      {
        type: 'bar',
        name: '去重点击',
        stack: 'clicks',
        barMaxWidth: 22,
        barCategoryGap: CHART_BAR_CATEGORY_GAP,
        data: plotted.value.map((r) => r.uniques),
        itemStyle: { color: t.accent, ...border }
      },
      {
        type: 'bar',
        name: '重复与爬虫',
        stack: 'clicks',
        barMaxWidth: 22,
        barCategoryGap: CHART_BAR_CATEGORY_GAP,
        data: plotted.value.map((r) => Math.max(0, r.total - r.uniques)),
        itemStyle: { color: t.muted, ...border }
      }
    ]
  }
})
</script>

<template>
  <div class="space-y-4">
    <ChartFrame
      :option="option"
      :height="rows.length * 46 + 56"
      :empty="!rows.length"
      empty-text="还没有应用铸出链接"
    />

    <div v-if="rows.length" class="overflow-x-auto">
      <table class="w-full min-w-[32rem] text-sm">
        <thead>
          <tr class="border-default-200 text-default-400 border-b text-left">
            <th class="px-4 py-2 font-medium">应用</th>
            <th class="px-4 py-2 text-right font-medium">链接数</th>
            <th class="px-4 py-2 text-right font-medium">去重点击</th>
            <th class="px-4 py-2 text-right font-medium">总点击</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, i) in rows"
            :key="row.client_id"
            class="border-default-100 border-b"
            :class="i === rows.length - 1 && 'border-b-0'"
          >
            <td class="px-4 py-2">
              <NuxtLink
                :to="`/dashboard/apps/${row.client_id}`"
                class="text-foreground hover:text-primary font-medium hover:underline"
              >
                {{ row.name }}
              </NuxtLink>
            </td>
            <td class="text-default-500 px-4 py-2 text-right tabular-nums">
              {{ fmt(row.links) }}
            </td>
            <td
              class="text-foreground px-4 py-2 text-right font-medium tabular-nums"
            >
              {{ fmt(row.uniques) }}
            </td>
            <td class="text-default-400 px-4 py-2 text-right tabular-nums">
              {{ fmt(row.total) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
