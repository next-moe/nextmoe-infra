<script setup lang="ts">
import {
  CANDIDATE_REASON_LABELS,
  CANDIDATE_STATUS,
  CANDIDATE_STATUS_COLORS,
  CANDIDATE_STATUS_LABELS,
  CATALOG_SOURCE_LABELS
} from '~/constants/catalog'
import type {
  CatalogCandidateItem,
  CatalogEntitySummary
} from '~~/shared/types/catalog'

const props = defineProps<{ item: CatalogCandidateItem; busy: boolean }>()

const emit = defineEmits<{
  accept: []
  reject: []
  defer: []
  detach: []
}>()

const sides = computed(() => [
  { tag: 'A', summary: props.item.a },
  { tag: 'B', summary: props.item.b }
])

const isDecidable = computed(() =>
  (
    [
      CANDIDATE_STATUS.pending,
      CANDIDATE_STATUS.deferred,
      CANDIDATE_STATUS.needsManual
    ] as number[]
  ).includes(props.item.status)
)

const sourceLabel = (s: CatalogEntitySummary) =>
  s.source_id == null
    ? ''
    : (CATALOG_SOURCE_LABELS[s.source_id] ?? `源${s.source_id}`)
</script>

<template>
  <KunCard content-class="space-y-2 p-3">
    <div class="flex flex-wrap items-center gap-2">
      <KunChip color="info" variant="flat" size="xs">名义</KunChip>
      <KunChip color="default" variant="flat" size="xs">
        {{ CANDIDATE_REASON_LABELS[item.reason] ?? item.reason }}
      </KunChip>
      <KunChip
        v-if="item.score !== null"
        color="default"
        variant="flat"
        size="xs"
      >
        相似度 {{ item.score?.toFixed(2) }}
      </KunChip>
      <KunChip
        :color="CANDIDATE_STATUS_COLORS[item.status] ?? 'default'"
        variant="flat"
        size="xs"
        class="ml-auto"
      >
        {{ CANDIDATE_STATUS_LABELS[item.status] ?? item.status }}
      </KunChip>
    </div>

    <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
      <template v-for="(side, index) in sides" :key="side.tag">
        <KunIcon
          v-if="index > 0"
          name="lucide:arrow-left-right"
          class="text-default-400 size-4"
        />
        <span class="flex flex-wrap items-center gap-1.5">
          <span class="text-foreground font-medium">
            {{ side.summary.display_name || `#${side.summary.id}` }}
          </span>
          <span class="text-default-400 font-mono text-xs">
            #{{ side.summary.id }}
          </span>
          <KunChip
            v-if="side.summary.source_id != null"
            color="info"
            variant="flat"
            size="xs"
          >
            {{ sourceLabel(side.summary) }}
          </KunChip>
          <span class="text-default-400 text-xs">
            {{ side.summary.credit_count ?? 0 }} 条署名
          </span>
        </span>
      </template>
    </div>

    <div v-if="isDecidable" class="flex flex-wrap gap-2">
      <KunButton
        color="success"
        size="sm"
        :disabled="busy"
        @click="emit('accept')"
      >
        确认为同一人
      </KunButton>
      <KunButton
        color="danger"
        variant="flat"
        size="sm"
        :disabled="busy"
        @click="emit('reject')"
      >
        不是同一人（永久保留）
      </KunButton>
      <KunButton
        color="default"
        variant="flat"
        size="sm"
        :disabled="busy"
        @click="emit('defer')"
      >
        搁置
      </KunButton>
    </div>

    <div v-else class="flex flex-wrap items-center gap-2">
      <p class="text-default-400 text-sm">
        已决策（{{ CANDIDATE_STATUS_LABELS[item.status] }}
        <template v-if="item.decided_by !== null">
          · 操作者 {{ item.decided_by }}</template
        >）
      </p>
      <KunButton
        v-if="item.status === CANDIDATE_STATUS.accepted"
        color="default"
        variant="flat"
        size="sm"
        :disabled="busy"
        @click="emit('detach')"
      >
        撤销关联（摘离双方名义）
      </KunButton>
    </div>
  </KunCard>
</template>
