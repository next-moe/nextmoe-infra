<script setup lang="ts">
import type { EditQuota, EditQuotaCounter } from '#shared/types/quota'
import { QUOTA_TIER_LABELS } from '~/constants/quota'

const props = defineProps<{ quota: EditQuota | null }>()

const rows = computed<
  { label: string; hint: string; counter: EditQuotaCounter }[]
>(() => {
  const quota = props.quota
  if (!quota) {
    return []
  }
  return [
    {
      label: '提案',
      hint: `最近 ${quota.window_hours} 小时内提交`,
      counter: quota.proposals
    },
    {
      label: '免审生效',
      hint: '直接生效、无需审核的修改',
      counter: quota.direct_edits
    },
    {
      label: '同时待审',
      hint: '尚未处理的提案',
      counter: quota.open_proposals
    }
  ]
})
</script>

<template>
  <KunCard bordered padding="md">
    <div class="mb-3 flex items-center justify-between gap-2">
      <h2 class="font-semibold">编辑配额</h2>
      <KunChip v-if="props.quota" size="sm" variant="flat" color="primary">
        {{ labelOf(QUOTA_TIER_LABELS, props.quota.quota_tier) }}
      </KunChip>
    </div>

    <p v-if="!props.quota" class="text-default-400 text-sm">配额信息暂不可用</p>
    <dl v-else class="space-y-3">
      <div v-for="row in rows" :key="row.label">
        <div class="mb-1 flex items-baseline justify-between gap-3 text-sm">
          <dt>
            <span class="font-medium">{{ row.label }}</span>
            <span class="text-default-400 ml-2 text-xs">{{ row.hint }}</span>
          </dt>
          <dd class="text-default-600 shrink-0 tabular-nums">
            {{ quotaCounterLabel(row.counter) }}
          </dd>
        </div>
        <KunProgress
          v-if="row.counter.limit"
          :value="row.counter.used"
          :max="row.counter.limit"
          :color="quotaTone(row.counter)"
          size="sm"
          :aria-label="`${row.label}已用 ${row.counter.used}`"
        />
      </div>
    </dl>
  </KunCard>
</template>
