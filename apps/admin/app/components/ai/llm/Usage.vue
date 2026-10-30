<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import {
  LLM_API,
  LLM_USAGE_DAYS,
  LLM_USAGE_DAYS_TABS,
  LLM_USAGE_GROUP_LABEL,
  LLM_USAGE_GROUP_TABS,
  LLM_USAGE_GROUPS
} from '~/constants/llm'
import type {
  LlmUsageGroup,
  LlmUsageReport,
  LlmUsageRow
} from '~~/shared/types/llm'

const api = useApi('ai')

const daysTab = ref('7')
const groupTab = ref('provider')
const report = ref<LlmUsageReport | null>(null)
const loading = ref(false)
const failed = ref('')

const days = computed(
  () => LLM_USAGE_DAYS.find((d) => String(d) === daysTab.value) ?? 7
)
const group = computed<LlmUsageGroup>(
  () => LLM_USAGE_GROUPS.find((g) => g === groupTab.value) ?? 'provider'
)

const load = async () => {
  loading.value = true
  failed.value = ''
  try {
    const res = await api.get<LlmUsageReport>(`${LLM_API}/usage`, {
      days: days.value,
      group: group.value
    })
    if (res.code !== 0) {
      failed.value = res.message || '加载失败'
      return
    }
    report.value = res.data
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch([days, group], load)

const errorRate = (r: Pick<LlmUsageRow, 'calls' | 'errors'>) =>
  r.calls ? `${((r.errors / r.calls) * 100).toFixed(1)}%` : '—'

const rows = computed(() =>
  [...(report.value?.rows ?? [])].sort((a, b) => b.cost_nano - a.cost_nano)
)

const totals = computed(() => {
  const t = report.value?.total
  if (!t) return []
  return [
    { label: '调用', value: t.calls.toLocaleString() },
    { label: '错误率', value: errorRate(t), hint: `${t.errors} 次错误` },
    {
      label: 'Token',
      value: (t.input_tokens + t.output_tokens).toLocaleString(),
      hint: `输入 ${t.input_tokens.toLocaleString()} · 输出 ${t.output_tokens.toLocaleString()}`
    },
    { label: '费用', value: llmNanoUsd(t.cost_nano) }
  ]
})

const bars = computed(() => {
  const byDay = new Map((report.value?.daily ?? []).map((d) => [d.day, d]))
  const list = llmCstDays(days.value).map((day) => ({
    day,
    calls: byDay.get(day)?.calls ?? 0,
    errors: byDay.get(day)?.errors ?? 0,
    cost: byDay.get(day)?.cost_nano ?? 0
  }))
  const max = Math.max(...list.map((d) => d.cost), 1)
  return list.map((d) => ({ ...d, pct: (d.cost / max) * 100 }))
})

const barText = (b: {
  day: string
  cost: number
  calls: number
  errors: number
}) =>
  `${b.day} · ${llmNanoUsd(b.cost)} · ${b.calls} 次调用 · ${b.errors} 次错误`
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <KunTab
        v-model="daysTab"
        :items="LLM_USAGE_DAYS_TABS"
        variant="pills"
        size="sm"
        aria-label="时间范围"
      />
      <KunTab
        v-model="groupTab"
        :items="LLM_USAGE_GROUP_TABS"
        variant="pills"
        size="sm"
        aria-label="分组方式"
      />
    </div>
    <p class="text-default-400 text-xs">
      按北京时间统计;费用以上游报告的实际花费为准,没有报告时按路由标价估算。
    </p>

    <CommonFetchError v-if="failed" :message="failed" @retry="load" />

    <div v-if="loading && !report" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <template v-else-if="report">
      <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <KunCard
          v-for="t in totals"
          :key="t.label"
          class-name="justify-start p-4"
        >
          <div>
            <p class="text-default-500 text-xs">{{ t.label }}</p>
            <p class="text-foreground mt-1 text-xl font-semibold tabular-nums">
              {{ t.value }}
            </p>
            <p v-if="t.hint" class="text-default-400 mt-0.5 text-xs">
              {{ t.hint }}
            </p>
          </div>
        </KunCard>
      </div>

      <KunCard v-if="days > 1" class-name="p-4">
        <p class="text-default-500 mb-3 text-sm">每日费用</p>
        <div class="flex h-40 items-end gap-1">
          <div
            v-for="b in bars"
            :key="b.day"
            class="flex h-full flex-1 items-end"
            :title="barText(b)"
            :aria-label="barText(b)"
            role="img"
          >
            <div
              class="bg-primary w-full rounded-t-sm"
              :style="{ height: `${Math.max(b.pct, b.cost ? 2 : 0)}%` }"
            />
          </div>
        </div>
        <div class="text-default-400 mt-1 flex justify-between text-xs">
          <span>{{ bars[0]?.day.slice(5) }}</span>
          <span>{{ bars[bars.length - 1]?.day.slice(5) }}</span>
        </div>
      </KunCard>

      <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
        <table class="w-full min-w-[48rem] text-sm">
          <thead class="bg-content2 text-default-500">
            <tr>
              <th class="px-3 py-2 text-left font-medium">
                {{ LLM_USAGE_GROUP_LABEL[group] }}
              </th>
              <th class="px-3 py-2 text-right font-medium">调用</th>
              <th class="px-3 py-2 text-right font-medium">错误率</th>
              <th class="px-3 py-2 text-right font-medium">
                输入 / 输出 tokens
              </th>
              <th class="px-3 py-2 text-right font-medium">费用</th>
              <th class="px-3 py-2 text-right font-medium">平均耗时</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="r in rows"
              :key="r.key"
              class="border-default-200 border-t"
            >
              <td class="px-3 py-2">
                <p class="text-foreground break-all">{{ r.label || r.key }}</p>
                <p
                  v-if="r.label && r.label !== r.key"
                  class="text-default-400 font-mono text-xs"
                >
                  {{ r.key }}
                </p>
              </td>
              <td class="px-3 py-2 text-right tabular-nums">
                {{ r.calls.toLocaleString() }}
              </td>
              <td
                :class="
                  cn(
                    'px-3 py-2 text-right tabular-nums',
                    r.errors ? 'text-danger-600' : 'text-default-500'
                  )
                "
              >
                {{ errorRate(r) }}
              </td>
              <td class="text-default-500 px-3 py-2 text-right tabular-nums">
                {{ r.input_tokens.toLocaleString() }} /
                {{ r.output_tokens.toLocaleString() }}
              </td>
              <td class="text-foreground px-3 py-2 text-right tabular-nums">
                {{ llmNanoUsd(r.cost_nano) }}
              </td>
              <td class="text-default-500 px-3 py-2 text-right tabular-nums">
                {{ r.avg_ms ? `${r.avg_ms.toLocaleString()} ms` : '—' }}
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="6" class="text-default-400 px-3 py-8 text-center">
                这段时间没有调用
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>
