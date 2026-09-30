<script setup lang="ts">
import { formatTelemetryRate, formatTelemetryMs } from '~/constants/telemetry'

defineProps<{
  kpis: {
    sessions: number
    crashFree: number | null
    anrRate: number | null
    unhandledRate: number | null
    ttidP50: number | null
    ttidP90: number | null
    jankRatio: number | null
  }
}>()

const fmtInt = (n: number) => n.toLocaleString('zh-CN')
</script>

<template>
  <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
    <div class="border-default-200 bg-content1 rounded-xl border p-4">
      <p class="text-default-500 text-xs">会话数</p>
      <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
        {{ fmtInt(kpis.sessions) }}
      </p>
    </div>
    <div class="border-default-200 bg-content1 rounded-xl border p-4">
      <p class="text-default-500 text-xs">无崩溃会话率</p>
      <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
        {{ formatTelemetryRate(kpis.crashFree, kpis.sessions) }}
      </p>
      <p class="text-default-400 mt-0.5 text-xs">1 − 崩溃会话 / 会话</p>
    </div>
    <div class="border-default-200 bg-content1 rounded-xl border p-4">
      <p class="text-default-500 text-xs">ANR 率</p>
      <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
        {{ formatTelemetryRate(kpis.anrRate, kpis.sessions) }}
      </p>
    </div>
    <div class="border-default-200 bg-content1 rounded-xl border p-4">
      <p class="text-default-500 text-xs">未处理异常率</p>
      <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
        {{ formatTelemetryRate(kpis.unhandledRate, kpis.sessions) }}
      </p>
    </div>
    <div class="border-default-200 bg-content1 rounded-xl border p-4">
      <p class="text-default-500 text-xs">冷启动 TTID P50</p>
      <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
        {{ formatTelemetryMs(kpis.ttidP50) }}
      </p>
      <p class="text-default-400 mt-0.5 text-xs">按天加权</p>
    </div>
    <div class="border-default-200 bg-content1 rounded-xl border p-4">
      <p class="text-default-500 text-xs">冷启动 TTID P90</p>
      <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
        {{ formatTelemetryMs(kpis.ttidP90) }}
      </p>
      <p class="text-default-400 mt-0.5 text-xs">按天加权</p>
    </div>
    <div class="border-default-200 bg-content1 rounded-xl border p-4">
      <p class="text-default-500 text-xs">卡顿帧占比</p>
      <p class="text-foreground mt-1 text-2xl font-bold tabular-nums">
        {{
          kpis.jankRatio == null ? '—' : formatTelemetryRate(kpis.jankRatio, 1)
        }}
      </p>
    </div>
  </div>
</template>
