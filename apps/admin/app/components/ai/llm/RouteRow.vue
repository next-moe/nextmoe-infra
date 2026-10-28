<script setup lang="ts">
import { LLM_ROUTE_STATUS_META } from '~/constants/llm'
import type { LlmRoute } from '~~/shared/types/llm'

const props = defineProps<{
  route: LlmRoute
  providerName: string
  busy: string
}>()
const emit = defineEmits<{
  test: []
  toggle: []
  edit: []
  remove: []
}>()

const meta = computed(
  () =>
    LLM_ROUTE_STATUS_META[props.route.status] ?? {
      label: props.route.status,
      color: 'default' as const
    }
)
const stats = computed(() => props.route.stats_24h)
const errorRate = computed(() =>
  stats.value.calls
    ? `${((stats.value.errors / stats.value.calls) * 100).toFixed(1)}%`
    : '—'
)
const toggleLabel = computed(() =>
  props.route.status === 'active' ? '停用' : '启用'
)
const perCall = computed(() => props.route.price?.per_call_micro ?? 0)
const showTokenPrice = computed(() => {
  const p = props.route.price
  return (
    !p || !perCall.value || p.input_micro + p.cached_micro + p.output_micro > 0
  )
})
const isBusy = (what: string) => props.busy === `${props.route.id}:${what}`
</script>

<template>
  <tr class="border-default-200 border-t align-top">
    <td class="px-3 py-2">
      <p class="text-foreground font-medium">{{ providerName }}</p>
      <p class="text-default-500 font-mono text-xs break-all">
        {{ route.upstream_id }}
      </p>
      <KunChip color="default" variant="flat" size="xs" class-name="mt-1">
        {{ route.protocol }}
      </KunChip>
    </td>
    <td class="px-3 py-2 text-right tabular-nums">{{ route.priority }}</td>
    <td class="px-3 py-2 text-right tabular-nums">{{ route.weight }}</td>
    <td class="px-3 py-2">
      <KunTooltip
        v-if="route.status_reason"
        :text="route.status_reason"
        position="top"
      >
        <KunChip :color="meta.color" variant="flat" size="xs">
          {{ meta.label }}
          <KunIcon name="lucide:info" class="ml-1 size-3" />
        </KunChip>
      </KunTooltip>
      <KunChip v-else :color="meta.color" variant="flat" size="xs">
        {{ meta.label }}
      </KunChip>
    </td>
    <td class="px-3 py-2 text-xs tabular-nums">
      <template v-if="route.price">
        <template v-if="showTokenPrice">
          <p>输入 {{ llmUsdPerM(route.price.input_micro) }}</p>
          <p>缓存 {{ llmUsdPerM(route.price.cached_micro) }}</p>
          <p>输出 {{ llmUsdPerM(route.price.output_micro) }}</p>
        </template>
        <p v-if="perCall">每次 {{ llmUsdPerM(perCall) }}</p>
      </template>
      <span v-else class="text-default-400">未定价</span>
    </td>
    <td class="px-3 py-2 text-xs tabular-nums">
      <p>{{ stats.calls.toLocaleString() }} 次 · 错误 {{ errorRate }}</p>
      <p class="text-default-500">
        均 {{ stats.avg_ms.toLocaleString() }} ms ·
        {{ llmCostUsd(stats.cost_nano) }}
      </p>
    </td>
    <td class="px-3 py-2 text-right whitespace-nowrap">
      <KunButton
        size="sm"
        variant="flat"
        :loading="isBusy('test')"
        @click="emit('test')"
      >
        测试
      </KunButton>
      <KunButton
        size="sm"
        variant="light"
        :color="route.status === 'active' ? 'warning' : 'success'"
        :loading="isBusy('status')"
        @click="emit('toggle')"
      >
        {{ toggleLabel }}
      </KunButton>
      <KunButton size="sm" variant="light" @click="emit('edit')">
        编辑
      </KunButton>
      <KunButton
        size="sm"
        variant="light"
        color="danger"
        :loading="isBusy('delete')"
        @click="emit('remove')"
      >
        删除
      </KunButton>
    </td>
  </tr>
</template>
