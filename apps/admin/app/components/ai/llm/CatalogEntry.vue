<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { KunDropdownItem } from '@kungal/ui-vue'
import {
  LLM_CATALOG_KIND_META,
  LLM_CATALOG_KIND_SOURCE_LABEL,
  LLM_CATALOG_KINDS,
  LLM_ROUTE_STATUS_META
} from '~/constants/llm'
import type {
  LlmCatalogEntry,
  LlmCatalogKind,
  LlmRouteStatus
} from '~~/shared/types/llm'

const props = defineProps<{ entry: LlmCatalogEntry; busy: string }>()
const emit = defineEmits<{
  kind: [LlmCatalogKind]
  probe: []
  adopt: []
}>()

const kindMeta = (k: string) =>
  LLM_CATALOG_KIND_META[k as LlmCatalogKind] ?? {
    label: k,
    color: 'default' as const
  }
const routeMeta = (s: string) =>
  LLM_ROUTE_STATUS_META[s as LlmRouteStatus] ?? {
    label: s,
    color: 'default' as const
  }

const kindItems = computed<KunDropdownItem[]>(() =>
  LLM_CATALOG_KINDS.map((k) => ({
    key: k,
    label: `${k} · ${LLM_CATALOG_KIND_META[k].label}`,
    disabled: k === props.entry.kind
  }))
)
const pickKind = (item: KunDropdownItem) => {
  const k = LLM_CATALOG_KINDS.find((x) => x === item.key)
  if (k) emit('kind', k)
}

const unsupportedHint = computed(
  () => `网关暂不能调用「${kindMeta(props.entry.kind).label}」类模型`
)
const isBusy = (what: string) => props.busy === `${props.entry.id}:${what}`
</script>

<template>
  <div
    :class="
      cn(
        'flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-start sm:justify-between',
        !entry.listed && 'opacity-60'
      )
    "
  >
    <div class="min-w-0 flex-1 space-y-1.5">
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-foreground font-mono text-sm font-medium break-all">
          {{ entry.upstream_id }}
        </span>
        <KunChip :color="kindMeta(entry.kind).color" variant="flat" size="xs">
          {{ kindMeta(entry.kind).label }}
        </KunChip>
        <span class="text-default-400 text-xs">
          {{ LLM_CATALOG_KIND_SOURCE_LABEL[entry.kind_source] }}
        </span>
        <KunChip v-if="!entry.listed" color="default" variant="flat" size="xs">
          已下架 · {{ llmDate(entry.delisted_at) }}
        </KunChip>
      </div>

      <div
        class="text-default-500 flex flex-wrap gap-x-3 gap-y-1 text-xs tabular-nums"
      >
        <span v-if="entry.owned_by">{{ entry.owned_by }}</span>
        <span v-if="entry.context_tokens > 0">
          上下文 {{ entry.context_tokens.toLocaleString() }}
        </span>
        <span v-if="entry.input_modalities.length">
          输入 {{ entry.input_modalities.join(' / ') }}
        </span>
        <span v-if="entry.output_modalities.length">
          输出 {{ entry.output_modalities.join(' / ') }}
        </span>
      </div>

      <p
        v-if="entry.probe"
        :class="
          cn(
            'flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs',
            entry.probe.ok ? 'text-success-600' : 'text-danger-600'
          )
        "
      >
        <KunIcon
          :name="entry.probe.ok ? 'lucide:circle-check' : 'lucide:circle-x'"
          class="size-3.5 shrink-0"
        />
        <span class="font-medium">
          {{ entry.probe.ok ? '测试通过' : '测试失败' }}
        </span>
        <span>{{ kindMeta(entry.probe.kind).label }}</span>
        <span class="tabular-nums">{{ entry.probe.latency_ms }} ms</span>
        <span v-if="entry.probe.error_kind" class="font-mono">
          [{{ entry.probe.error_kind }}]
        </span>
        <span v-if="entry.probe.message" class="break-all">
          {{ entry.probe.message }}
        </span>
        <span class="text-default-400">{{ llmTime(entry.probe.at) }}</span>
      </p>

      <div v-if="entry.routes.length" class="flex flex-wrap items-center gap-1">
        <span class="text-default-400 text-xs">已用于</span>
        <KunChip
          v-for="r in entry.routes"
          :key="r.route_id"
          :color="routeMeta(r.status).color"
          variant="flat"
          size="xs"
        >
          #{{ r.route_id }} {{ r.model_key }} · {{ routeMeta(r.status).label }}
        </KunChip>
      </div>
    </div>

    <div class="flex shrink-0 flex-wrap items-center gap-1">
      <KunDropdown :items="kindItems" position="bottom-end" @select="pickKind">
        <template #trigger>
          <KunButton size="sm" variant="light" :loading="isBusy('kind')">
            改类型
          </KunButton>
        </template>
      </KunDropdown>
      <KunTooltip
        v-if="!entry.supported"
        :text="unsupportedHint"
        position="top"
      >
        <KunButton size="sm" variant="flat" disabled>测试</KunButton>
      </KunTooltip>
      <KunButton
        v-else
        size="sm"
        variant="flat"
        :loading="isBusy('probe')"
        @click="emit('probe')"
      >
        测试
      </KunButton>
      <KunTooltip
        v-if="!entry.supported"
        :text="unsupportedHint"
        position="top"
      >
        <KunButton size="sm" variant="flat" color="primary" disabled>
          加入网关
        </KunButton>
      </KunTooltip>
      <KunButton
        v-else
        size="sm"
        variant="flat"
        color="primary"
        @click="emit('adopt')"
      >
        加入网关
      </KunButton>
    </div>
  </div>
</template>
