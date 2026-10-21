<script setup lang="ts">
import type { KunUIColor } from '@kungal/ui-core'
import type { CatalogMergeRequest } from '#shared/types/catalog'
import type { EditRefFamily } from '#shared/types/edit'
import { MERGE_STATE_LABELS } from '~/constants/creation'
import { ENTITY_META, isEditFamily } from '~/constants/editEntities'
import { mergeTiming } from '~/utils/creation'

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
  withdrawn: 'default'
}

const timing = computed(() =>
  mergeTiming(props.request.state, props.request.execute_after, props.now)
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
  <li class="space-y-2 py-3">
    <div class="flex flex-wrap items-center gap-2 text-sm">
      <KunChip size="sm" variant="flat">{{
        meta?.label ?? request.target_object
      }}</KunChip>
      <NuxtLink
        :to="`${meta?.path}/${request.source_id}`"
        class="hover:text-primary font-medium"
      >
        {{ nameOf(request.source_id) }}
      </NuxtLink>
      <KunIcon name="lucide:arrow-right" class="text-default-400 size-4" />
      <NuxtLink
        :to="`${meta?.path}/${request.target_id}`"
        class="hover:text-primary font-medium"
      >
        {{ nameOf(request.target_id) }}
      </NuxtLink>
      <KunChip
        size="sm"
        variant="flat"
        :color="STATE_COLORS[request.state] ?? 'default'"
        class-name="ml-auto"
      >
        {{ MERGE_STATE_LABELS[request.state] ?? request.state }}
      </KunChip>
    </div>
    <p
      v-if="request.note"
      class="text-default-600 bg-default-50 rounded-lg p-2 text-sm whitespace-pre-line"
    >
      {{ request.note }}
    </p>
    <div class="text-default-400 flex flex-wrap items-center gap-2 text-xs">
      <HistoryAuthor
        :user="request.proposed_by"
        :uid="request.proposed_by_uid"
        :origin="request.origin_site"
      />
      <span>提交于 {{ formatDateTime(request.proposed_at) }}</span>
      <span v-if="request.state === 'approved' && timing.waitMs > 0">
        · 冷却期还剩 {{ formatWait(timing.waitMs) }}
      </span>
      <span v-else-if="timing.canExecute">· 冷却期已结束，等待执行</span>
      <span v-if="request.executed_at">
        · 执行于 {{ formatDateTime(request.executed_at) }}
      </span>
    </div>
    <slot :timing="timing" />
  </li>
</template>
