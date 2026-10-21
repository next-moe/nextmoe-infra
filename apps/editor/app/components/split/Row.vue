<script setup lang="ts">
import type { KunUIColor } from '@kungal/ui-core'
import type { CatalogSplitRequest } from '#shared/types/catalog'
import { MERGE_STATE_LABELS } from '~/constants/creation'
import { mergeTiming } from '~/utils/creation'

const props = defineProps<{ request: CatalogSplitRequest; now: number }>()

const STATE_COLORS: Record<string, KunUIColor> = {
  open: 'warning',
  approved: 'primary',
  executed: 'success',
  rejected: 'danger',
  withdrawn: 'default',
  undone: 'secondary'
}
const STATE_LABELS: Record<string, string> = {
  ...MERGE_STATE_LABELS,
  executed: '已拆分'
}

const timing = computed(() =>
  mergeTiming(props.request.state, props.request.execute_after, props.now)
)
</script>

<template>
  <li>
    <NuxtLink
      :to="`/split/${props.request.id}`"
      target="_blank"
      class="hover:bg-default-50 -mx-2 block space-y-1.5 rounded-lg px-2 py-3 transition-colors"
    >
      <div class="flex flex-wrap items-center gap-2 text-sm">
        <span class="font-medium">{{ props.request.target_name }}</span>
        <KunIcon name="lucide:split" class="text-default-400 size-4" />
        <span class="font-medium">
          {{ props.request.new_work_name || props.request.display_name }}
        </span>
        <span class="text-default-500 text-xs">
          {{ props.request.release_ids.length }} 个发行
        </span>
        <span class="text-default-400 text-xs">#{{ props.request.id }}</span>
        <KunChip
          size="sm"
          variant="flat"
          :color="STATE_COLORS[props.request.state] ?? 'default'"
          class-name="ml-auto"
        >
          {{ STATE_LABELS[props.request.state] ?? props.request.state }}
        </KunChip>
      </div>
      <p class="text-default-600 line-clamp-2 text-sm">
        {{ props.request.note }}
      </p>
      <div class="text-default-400 flex flex-wrap items-center gap-2 text-xs">
        <HistoryAuthor
          :user="props.request.proposed_by"
          :uid="props.request.proposed_by_uid"
          :origin="props.request.origin_site"
          :linked="false"
        />
        <span>提交于 {{ formatDateTime(props.request.proposed_at) }}</span>
        <span v-if="props.request.state === 'approved' && timing.waitMs > 0">
          · 冷静期还剩 {{ formatWait(timing.waitMs) }}
        </span>
        <span v-else-if="timing.canExecute">· 冷静期已结束，等待执行</span>
        <span v-if="props.request.executed_at">
          · 执行于 {{ formatDateTime(props.request.executed_at) }}
        </span>
      </div>
    </NuxtLink>
  </li>
</template>
