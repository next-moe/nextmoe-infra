<script setup lang="ts">
import { AI_WINDOW_OPTIONS, type AiWindow } from '~/constants/ai'
import type { AiUsageSummary } from '~~/shared/types/ai'

const win = ref<AiWindow>('24h')

const { data, status } = await useApiFetch<AiUsageSummary>(
  '/admin/ai/usage/summary',
  { query: computed(() => ({ window: win.value })) },
  'ai'
)
const overview = computed(() => data.value?.overview)
const rows = computed(() => data.value?.rows ?? [])
const isLoading = computed(() => status.value === 'pending')

const fmtInt = (n?: number) => (n ?? 0).toLocaleString()
const fmtPct = (n?: number) => `${((n ?? 0) * 100).toFixed(1)}%`
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-foreground text-2xl font-bold">AI 网关用量</h1>
      <div class="flex flex-wrap gap-2">
        <KunButton
          v-for="opt in AI_WINDOW_OPTIONS"
          :key="opt.value"
          size="sm"
          :variant="win === opt.value ? 'flat' : 'light'"
          :color="win === opt.value ? 'primary' : 'default'"
          @click="win = opt.value"
        >
          {{ opt.label }}
        </KunButton>
      </div>
    </div>

    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
      <div class="border-default-200 bg-content1 rounded-xl border p-4">
        <p class="text-default-500 text-xs">调用数</p>
        <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
          {{ fmtInt(overview?.calls) }}
        </p>
        <p class="text-default-400 mt-0.5 text-xs">
          tokens
          {{
            fmtInt(
              (overview?.prompt_tokens ?? 0) +
                (overview?.completion_tokens ?? 0)
            )
          }}
        </p>
      </div>
      <div class="border-default-200 bg-content1 rounded-xl border p-4">
        <p class="text-default-500 text-xs">错误率</p>
        <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
          {{ fmtPct(overview?.error_rate) }}
        </p>
        <p class="text-default-400 mt-0.5 text-xs">非正常 / 总调用</p>
      </div>
      <div class="border-default-200 bg-content1 rounded-xl border p-4">
        <p class="text-default-500 text-xs">降级调用</p>
        <p class="text-warning-600 mt-1 text-2xl font-bold tabular-nums">
          {{ fmtInt(overview?.degraded) }}
        </p>
        <p class="text-default-400 mt-0.5 text-xs">上游未接入前为常态</p>
      </div>
      <div class="border-default-200 bg-content1 rounded-xl border p-4">
        <p class="text-default-500 text-xs">回复被截断</p>
        <p class="text-danger-600 mt-1 text-2xl font-bold tabular-nums">
          {{ fmtInt(overview?.truncated) }}
        </p>
        <p class="text-default-400 mt-0.5 text-xs">非零即 max_tokens 过小</p>
      </div>
      <div class="border-default-200 bg-content1 rounded-xl border p-4">
        <p class="text-default-500 text-xs">成本 (µ)</p>
        <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
          {{ fmtInt(overview?.cost_micro) }}
        </p>
        <p class="text-default-400 mt-0.5 text-xs">微币;定价接入前为 0</p>
      </div>
    </div>

    <AiSummaryTable :rows="rows" :loading="isLoading" />
    <AiTrend />
    <AiBudgetPanel />
  </div>
</template>
