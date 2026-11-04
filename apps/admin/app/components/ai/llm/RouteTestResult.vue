<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { LlmRouteTestResult } from '~~/shared/types/llm'

defineProps<{ result: LlmRouteTestResult }>()
</script>

<template>
  <div
    :class="
      cn(
        'flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-xs',
        result.ok
          ? 'border-success-200 bg-success-50 text-success-700'
          : 'border-danger-200 bg-danger-50 text-danger-700'
      )
    "
  >
    <KunIcon
      :name="result.ok ? 'lucide:circle-check' : 'lucide:circle-x'"
      class="size-4 shrink-0"
    />
    <span class="font-medium">{{ result.ok ? '测试通过' : '测试失败' }}</span>
    <span class="tabular-nums">{{ result.latency_ms }} ms</span>
    <span v-if="result.served_model" class="font-mono">
      {{ result.served_model }}
    </span>
    <span v-if="result.error_kind" class="font-mono">
      [{{ result.error_kind }}]
    </span>
    <span v-if="result.message" class="break-all">{{ result.message }}</span>
  </div>
</template>
