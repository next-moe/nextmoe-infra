<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogDeletionRequest } from '#shared/types/catalog'
import {
  DELETION_REASON_LABELS,
  DELETION_STATE_COLORS,
  DELETION_STATE_LABELS
} from '~/constants/deletion'
import { mergeTiming } from '~/utils/creation'

const props = withDefaults(
  defineProps<{
    request: CatalogDeletionRequest
    now: number
    selected?: boolean
  }>(),
  { selected: false }
)
const emit = defineEmits<{ select: [] }>()

const timing = computed(() =>
  mergeTiming(props.request.state, props.request.execute_after, props.now)
)
const name = computed(
  () => props.request.target_name || `作品 #${props.request.target_id}`
)
</script>

<template>
  <li>
    <button
      type="button"
      :aria-current="props.selected ? 'true' : undefined"
      :class="
        cn(
          'w-full space-y-1.5 rounded-lg px-3 py-2.5 text-left transition-colors',
          props.selected ? 'bg-primary-50' : 'hover:bg-default-100'
        )
      "
      @click="emit('select')"
    >
      <div class="flex items-center gap-2">
        <span class="min-w-0 flex-1 truncate text-sm font-medium">
          {{ name }}
        </span>
        <KunChip
          size="sm"
          variant="flat"
          :color="DELETION_STATE_COLORS[props.request.state] ?? 'default'"
        >
          {{
            DELETION_STATE_LABELS[props.request.state] ?? props.request.state
          }}
        </KunChip>
      </div>
      <div
        class="text-default-500 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs"
      >
        <span class="text-default-700">
          {{
            DELETION_REASON_LABELS[props.request.reason] ?? props.request.reason
          }}
        </span>
        <span>·</span>
        <span>{{
          props.request.proposed_by?.name ??
          `用户 #${props.request.proposed_by_uid}`
        }}</span>
        <span>· {{ formatRelative(props.request.proposed_at) }}</span>
        <span
          v-if="props.request.state === 'approved' && timing.waitMs > 0"
          class="text-primary-600"
        >
          · 冷静期还剩 {{ formatWait(timing.waitMs) }}
        </span>
        <span v-else-if="timing.canExecute" class="text-warning-600">
          · 可以执行
        </span>
      </div>
    </button>
  </li>
</template>
