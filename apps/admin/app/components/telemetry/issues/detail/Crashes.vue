<script setup lang="ts">
import { formatTelemetryNeeds } from '~/constants/telemetry'
import type { TelemetryIssueCrash } from '~~/shared/types/telemetry'

const props = defineProps<{ crashes: TelemetryIssueCrash[] }>()

const rawOpen = ref<Record<number, boolean>>({})
const toggleRaw = (i: number) => {
  rawOpen.value = { ...rawOpen.value, [i]: !rawOpen.value[i] }
}

const breadcrumbs = (c: TelemetryIssueCrash) => c['app.breadcrumbs'] ?? []

const handledLabel = (v: boolean | null) => {
  if (v === true) return '已捕获'
  if (v === false) return '未捕获'
  return '—'
}
</script>

<template>
  <div class="space-y-3">
    <h2 class="text-foreground text-lg font-semibold">最近崩溃</h2>
    <p v-if="!props.crashes.length" class="text-default-400 text-sm">
      还没有崩溃样本
    </p>
    <div
      v-for="(c, i) in props.crashes"
      :key="`${c.event_day}-${i}`"
      class="border-default-200 bg-content1 space-y-3 rounded-xl border p-4"
    >
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-foreground font-mono text-sm">
          {{ c.service_version }}
        </span>
        <span class="text-default-500 text-sm tabular-nums">
          {{ c.event_day }}
        </span>
        <TelemetryCrashStatusChip :status="c.status" />
        <span
          v-if="formatTelemetryNeeds(c.needs)"
          class="text-warning-600 text-xs"
        >
          {{ formatTelemetryNeeds(c.needs) }}
        </span>
      </div>
      <dl
        class="text-default-500 grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-4"
      >
        <div>
          <dt class="text-default-400">机型</dt>
          <dd class="text-foreground">{{ c.device_model || '—' }}</dd>
        </div>
        <div>
          <dt class="text-default-400">系统</dt>
          <dd class="text-foreground">
            {{ c.os_version || '—'
            }}<span v-if="c.api_level != null"> / API {{ c.api_level }}</span>
          </dd>
        </div>
        <div>
          <dt class="text-default-400">架构</dt>
          <dd class="text-foreground font-mono">{{ c.host_arch || '—' }}</dd>
        </div>
        <div>
          <dt class="text-default-400">捕获</dt>
          <dd class="text-foreground">{{ handledLabel(c.handled) }}</dd>
        </div>
      </dl>
      <div>
        <p class="text-foreground font-mono text-sm break-all">
          {{ c.exception_type || '—' }}
        </p>
        <p class="text-default-500 mt-1 text-sm break-all">
          {{ c.message || '—' }}
        </p>
      </div>
      <pre
        class="bg-default-50 max-h-64 overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap"
        >{{ c.stack || '（无解码栈）' }}</pre>
      <KunButton
        v-if="c.raw_stack"
        size="sm"
        variant="light"
        @click="toggleRaw(i)"
      >
        {{ rawOpen[i] ? '收起原始栈' : '查看原始栈' }}
      </KunButton>
      <pre
        v-if="rawOpen[i]"
        class="bg-default-50 max-h-64 overflow-auto rounded-lg p-3 font-mono text-xs whitespace-pre-wrap"
        >{{ c.raw_stack }}</pre>
      <div v-if="breadcrumbs(c).length">
        <p class="text-default-500 mb-1 text-xs">面包屑</p>
        <ol
          class="text-default-500 list-inside list-decimal space-y-0.5 text-xs"
        >
          <li v-for="(b, bi) in breadcrumbs(c)" :key="bi" class="break-all">
            {{ b }}
          </li>
        </ol>
      </div>
    </div>
  </div>
</template>
