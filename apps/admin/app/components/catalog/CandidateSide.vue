<script setup lang="ts">
import {
  CATALOG_MEDIUM_COLORS,
  CATALOG_MEDIUM_LABELS,
  CLAIM_STATE_LABELS,
  CONTENT_RATING_COLORS,
  CONTENT_RATING_LABELS,
  WORK_STATUS,
  WORK_STATUS_LABELS
} from '~/constants/catalog'
import type { CatalogEntitySummary } from '~~/shared/types/catalog'

const props = defineProps<{
  tag: 'A' | 'B'
  entityType: number
  summary: CatalogEntitySummary
  sharedKeys: string[]
  role: 'keep' | 'absorb'
  nameMismatch: boolean
}>()

const refs = computed(() => props.summary.refs ?? [])

const isShared = (sourceId: number, externalId: string) =>
  props.sharedKeys.includes(`${sourceId}:${externalId}`)

const isQuarantined = computed(
  () => props.summary.work_status === WORK_STATUS.QUARANTINE
)

const abnormalStatus = computed(() =>
  props.summary.work_status != null &&
  props.summary.work_status !== WORK_STATUS.LIVE
    ? props.summary.work_status
    : null
)
</script>

<template>
  <div
    class="rounded-lg border p-3"
    :class="
      role === 'keep'
        ? 'border-success-300 bg-success-50'
        : 'border-default-200'
    "
  >
    <div class="mb-1 flex items-center gap-2">
      <KunChip
        :color="role === 'keep' ? 'success' : 'default'"
        :variant="role === 'keep' ? 'solid' : 'flat'"
        size="xs"
      >
        {{ tag }} · {{ role === 'keep' ? '保留' : '并入' }}
      </KunChip>
      <span class="text-default-400 font-mono text-xs">#{{ summary.id }}</span>
      <KunCopy :text="String(summary.id)" />
    </div>

    <div class="flex flex-wrap items-center gap-1.5">
      <p
        class="text-lg leading-tight font-medium"
        :class="nameMismatch ? 'text-warning-700' : 'text-foreground'"
      >
        {{ summary.display_name || '（无显示名）' }}
      </p>
      <KunChip
        v-if="abnormalStatus !== null"
        :color="isQuarantined ? 'warning' : 'default'"
        variant="flat"
        size="xs"
      >
        {{ WORK_STATUS_LABELS[abnormalStatus] ?? abnormalStatus }}
      </KunChip>
    </div>

    <div class="mt-2 flex flex-wrap items-center gap-1.5">
      <KunChip
        v-if="summary.medium_id != null"
        :color="CATALOG_MEDIUM_COLORS[summary.medium_id] ?? 'default'"
        variant="flat"
        size="xs"
      >
        {{
          CATALOG_MEDIUM_LABELS[summary.medium_id] ?? `媒介${summary.medium_id}`
        }}
      </KunChip>
      <KunChip
        v-if="summary.content_rating != null"
        :color="CONTENT_RATING_COLORS[summary.content_rating] ?? 'default'"
        variant="flat"
        size="xs"
      >
        {{
          CONTENT_RATING_LABELS[summary.content_rating] ??
          summary.content_rating
        }}
      </KunChip>
      <span v-if="summary.olang" class="text-default-500 text-xs">
        原语 <span class="font-mono">{{ summary.olang }}</span>
      </span>
      <span
        v-if="summary.release_year != null"
        class="text-default-500 text-xs"
      >
        {{ summary.release_year }} 年发行
      </span>
      <span
        v-if="summary.credit_count != null"
        class="text-default-500 text-xs"
      >
        {{ summary.credit_count }} 条署名
      </span>
    </div>

    <div v-if="summary.site" class="mt-2">
      <KunChip color="warning" variant="flat" size="sm">
        <template #start>
          <KunIcon name="lucide:shield-check" class="mr-1 size-3.5" />
        </template>
        已被 {{ summary.site }} 认领<template
          v-if="summary.claim_state != null"
        >
          · {{ CLAIM_STATE_LABELS[summary.claim_state] ?? summary.claim_state }}
        </template>
      </KunChip>
    </div>

    <div class="mt-2">
      <p class="text-default-400 mb-1 text-xs">外部锚点</p>
      <div v-if="refs.length" class="flex flex-wrap gap-1.5">
        <CatalogRefChip
          v-for="r in refs"
          :key="`${r.source_id}:${r.external_id}`"
          :source-id="r.source_id"
          :external-id="r.external_id"
          :link-kind="r.link_kind"
          :entity-type="entityType"
          :shared="isShared(r.source_id, r.external_id)"
        />
      </div>
      <p v-else class="text-default-300 text-xs">无（无法用外部身份佐证）</p>
    </div>
  </div>
</template>
