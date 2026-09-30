<script setup lang="ts">
import {
  TELEMETRY_ENGINE_STATUS_META,
  TELEMETRY_SYMBOL_KIND_LABELS
} from '~/constants/telemetry'
import type { TelemetrySymbolUpload } from '~~/shared/types/telemetry'

const props = defineProps<{ appId: number }>()

const { data, status, error, refresh } = await useApiFetch<
  TelemetrySymbolUpload[]
>(
  () => `/admin/telemetry/apps/${props.appId}/symbol-uploads`,
  { query: { limit: 20 } },
  'telemetry'
)

const uploads = computed(() => data.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const kindLabel = (kind: string) => TELEMETRY_SYMBOL_KIND_LABELS[kind] ?? kind
const engineMeta = (s: string) =>
  TELEMETRY_ENGINE_STATUS_META[s] ?? {
    label: s,
    color: 'default' as const
  }
</script>

<template>
  <div class="space-y-2">
    <h3 class="text-foreground text-sm font-semibold">符号上传</h3>
    <CommonFetchError v-if="error" :message="error.message" @retry="refresh" />
    <p v-else-if="isLoading" class="text-default-400 text-xs">加载中…</p>
    <p v-else-if="!uploads.length" class="text-default-400 text-xs">
      还没有符号包。CI 配置 TELEMETRY_SYMBOLS_TOKEN 后会显示在这里。
    </p>
    <div v-else class="space-y-3">
      <div
        v-for="u in uploads"
        :key="u.id"
        class="border-default-200 space-y-2 rounded-lg border p-3"
      >
        <div class="flex flex-wrap items-baseline gap-2">
          <span class="text-foreground font-mono text-sm">
            {{ u.service_version }}
          </span>
          <span class="text-default-400 text-xs">
            {{ new Date(u.created_at).toLocaleString('zh-CN') }}
          </span>
        </div>
        <p v-if="u.engine_revision" class="text-default-500 text-xs">
          引擎修订 {{ u.engine_revision }}
        </p>
        <ul class="text-default-500 space-y-1 text-xs">
          <li v-for="(f, i) in u.files" :key="`${u.id}-${i}`">
            {{ kindLabel(f.kind) }}
            <span v-if="f.arch"> · {{ f.arch }}</span>
            <span v-if="f.build_id" class="font-mono">
              · {{ f.build_id }}
            </span>
            · {{ formatFileSize(f.size) }}
          </li>
        </ul>
        <div v-if="u.engine_symbols?.length" class="flex flex-wrap gap-1">
          <KunChip
            v-for="e in u.engine_symbols"
            :key="`${e.variant}-${e.build_id}`"
            :color="engineMeta(e.status).color"
            variant="flat"
            size="xs"
          >
            {{ e.variant }} · {{ engineMeta(e.status).label
            }}<template v-if="e.last_error"> · {{ e.last_error }}</template>
          </KunChip>
        </div>
      </div>
    </div>
  </div>
</template>
