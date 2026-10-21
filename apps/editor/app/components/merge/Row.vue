<script setup lang="ts">
import type { KunUIColor } from '@kungal/ui-core'
import type { CatalogMergeRequest } from '#shared/types/catalog'
import type { EditRefFamily } from '#shared/types/edit'
import { MERGE_STATE_LABELS } from '~/constants/creation'
import { ENTITY_META, isEditFamily } from '~/constants/editEntities'
import { mergeTiming } from '~/utils/creation'
import { noteEntries } from '~/utils/merge/timeline'

const props = defineProps<{ request: CatalogMergeRequest; now: number }>()

const refs = useRefNames()

const family = computed(() =>
  isEditFamily(props.request.target_object) ? props.request.target_object : null
)
const meta = computed(() => (family.value ? ENTITY_META[family.value] : null))
const nameOf = (id: string) =>
  (family.value && refs.nameOf(family.value as EditRefFamily, id)) || `#${id}`

const STATE_COLORS: Record<string, KunUIColor> = {
  open: 'warning',
  approved: 'primary',
  executed: 'success',
  rejected: 'danger',
  withdrawn: 'default',
  undone: 'secondary'
}

const timing = computed(() =>
  mergeTiming(props.request.state, props.request.execute_after, props.now)
)
const reason = computed(
  () =>
    noteEntries(props.request.note).find((entry) => entry.kind === 'reason')
      ?.text ?? ''
)

watch(
  () => props.request,
  (request) => {
    if (family.value) {
      refs.resolve(family.value as EditRefFamily, [
        request.source_id,
        request.target_id
      ])
    }
  },
  { immediate: true }
)
</script>

<template>
  <li>
    <NuxtLink
      :to="`/merge/${request.id}`"
      class="hover:bg-default-50 -mx-2 block space-y-1.5 rounded-lg px-2 py-3 transition-colors"
    >
      <div class="flex flex-wrap items-center gap-2 text-sm">
        <KunChip size="sm" variant="flat">
          {{ meta?.label ?? request.target_object }}
        </KunChip>
        <span class="font-medium">{{ nameOf(request.source_id) }}</span>
        <KunIcon name="lucide:arrow-right" class="text-default-400 size-4" />
        <span class="font-medium">{{ nameOf(request.target_id) }}</span>
        <span class="text-default-400 text-xs">#{{ request.id }}</span>
        <KunChip
          size="sm"
          variant="flat"
          :color="STATE_COLORS[request.state] ?? 'default'"
          class-name="ml-auto"
        >
          {{ MERGE_STATE_LABELS[request.state] ?? request.state }}
        </KunChip>
      </div>
      <p v-if="reason" class="text-default-600 line-clamp-2 text-sm">
        {{ reason }}
      </p>
      <div class="text-default-400 flex flex-wrap items-center gap-2 text-xs">
        <HistoryAuthor
          :user="request.proposed_by"
          :uid="request.proposed_by_uid"
          :origin="request.origin_site"
          :linked="false"
        />
        <span>提交于 {{ formatDateTime(request.proposed_at) }}</span>
        <span v-if="request.state === 'approved' && timing.waitMs > 0">
          · 冷静期还剩 {{ formatWait(timing.waitMs) }}
        </span>
        <span v-else-if="timing.canExecute">· 冷静期已结束，等待执行</span>
        <span v-if="request.executed_at">
          · 执行于 {{ formatDateTime(request.executed_at) }}
        </span>
        <span
          v-if="request.state === 'executed' && request.undo_until"
          class="text-default-400"
        >
          · 可撤销至 {{ formatDateTime(request.undo_until) }}
        </span>
      </div>
    </NuxtLink>
    <slot :timing="timing" />
  </li>
</template>
