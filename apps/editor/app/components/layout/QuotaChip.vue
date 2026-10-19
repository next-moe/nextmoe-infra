<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { EditQuota, EditQuotaCounter } from '#shared/types/quota'
import { QUOTA_TIER_LABELS } from '~/constants/quota'

const api = useEditorApi()

const { data: quota } = await useAsyncData('editor-quota', () =>
  api<EditQuota>('/v2/me/edit-quota').catch(() => null)
)

const counterText = (counter: EditQuotaCounter) =>
  counter.limit === null
    ? `${counter.used} / 不限`
    : `${counter.used} / ${counter.limit}`

const exhausted = computed(() => {
  const value = quota.value
  return value
    ? [value.proposals, value.open_proposals].some(
        (counter) => counter.remaining === 0
      )
    : false
})

const rows = computed(() =>
  quota.value
    ? [
        {
          label: `提案（${quota.value.window_hours} 小时内）`,
          counter: quota.value.proposals
        },
        { label: '免审生效', counter: quota.value.direct_edits },
        { label: '待审中', counter: quota.value.open_proposals }
      ]
    : []
)
</script>

<template>
  <KunPopover
    v-if="quota"
    position="bottom-end"
    trigger="hover"
    aria-label="编辑配额"
  >
    <template #trigger>
      <span
        :class="
          cn(
            'hidden h-7 items-center gap-1.5 rounded-full px-2.5 text-xs tabular-nums md:inline-flex',
            exhausted
              ? 'bg-danger-50 text-danger-600'
              : 'bg-default-100 text-default-600'
          )
        "
      >
        <KunIcon name="lucide:gauge" class="size-3.5" />
        {{ counterText(quota.proposals) }}
      </span>
    </template>
    <div class="w-56 space-y-2 p-3 text-sm">
      <p class="font-medium">
        编辑配额 · {{ labelOf(QUOTA_TIER_LABELS, quota.quota_tier) }}
      </p>
      <dl class="space-y-1">
        <div
          v-for="row in rows"
          :key="row.label"
          class="flex justify-between gap-3"
        >
          <dt class="text-default-500">{{ row.label }}</dt>
          <dd
            :class="
              cn(
                'tabular-nums',
                row.counter.remaining === 0
                  ? 'text-danger-600'
                  : 'text-foreground'
              )
            "
          >
            {{ counterText(row.counter) }}
          </dd>
        </div>
      </dl>
    </div>
  </KunPopover>
</template>
