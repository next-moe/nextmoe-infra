<script setup lang="ts">
import type { EChartsOption } from 'echarts'
import { CHART_BAR_CATEGORY_GAP, CHART_STACK_GAP } from '~/constants/chart'
import type { StoreUsageDay } from '~~/shared/types/store'

const props = defineProps<{ days: StoreUsageDay[] }>()

const theme = useChartTheme()

const average = computed(() => {
  if (!props.days.length) return 0
  const sum = props.days.reduce((acc, d) => acc + d.uniques, 0)
  return Math.round(sum / props.days.length)
})

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
    grid: chartGrid({ top: 36 }),
    tooltip: {
      ...(chartBase(t).tooltip as object),
      formatter: (params: unknown) => {
        const rows = params as { dataIndex: number }[]
        const d = props.days[rows[0]!.dataIndex]
        if (!d) return ''
        return [
          `<span style="font-family:monospace">${d.day}</span>`,
          `去重 <b>${d.uniques.toLocaleString()}</b>`,
          `重复与爬虫 ${(d.total - d.uniques).toLocaleString()}`,
          `合计 ${d.total.toLocaleString()}`
        ].join('<br/>')
      }
    },
    xAxis: {
      ...categoryAxis(t),
      data: props.days.map((d) => d.day.slice(5))
    },
    yAxis: valueAxis(t),
    series: [
      {
        type: 'bar',
        name: '去重点击',
        stack: 'clicks',
        barMaxWidth: 22,
        barCategoryGap: CHART_BAR_CATEGORY_GAP,
        data: props.days.map((d) => d.uniques),
        itemStyle: { color: t.accent, ...border },
        markLine: average.value
          ? {
              silent: true,
              symbol: 'none',
              lineStyle: { color: t.ink, type: 'dashed', width: 1 },
              label: {
                position: 'insideEndTop',
                color: t.ink,
                fontSize: 11,
                backgroundColor: t.surface,
                padding: [2, 4],
                borderRadius: 3,
                formatter: `日均去重 ${average.value.toLocaleString()}`
              },
              data: [{ yAxis: average.value }]
            }
          : undefined
      },
      {
        type: 'bar',
        name: '重复与爬虫',
        stack: 'clicks',
        barMaxWidth: 22,
        barCategoryGap: CHART_BAR_CATEGORY_GAP,
        data: props.days.map((d) => Math.max(0, d.total - d.uniques)),
        itemStyle: { color: t.muted, ...border }
      }
    ]
  }
})
</script>

<template>
  <ChartFrame
    :option="option"
    :height="300"
    :empty="!days.length"
    empty-text="这段时间还没有点击"
  />
</template>
