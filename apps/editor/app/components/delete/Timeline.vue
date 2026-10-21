<script setup lang="ts">
import type { KunStepItem } from '@kungal/ui-vue'
import type { CatalogDeletionRequest } from '#shared/types/catalog'
import { mergeTiming } from '~/utils/creation'

const props = withDefaults(
  defineProps<{
    request: CatalogDeletionRequest
    now: number
    orientation?: 'horizontal' | 'vertical'
  }>(),
  { orientation: 'horizontal' }
)

const who = (name: string | undefined | null, at: string | null | undefined) =>
  [name, at ? formatDateTime(at) : ''].filter(Boolean).join(' · ')

const timing = computed(() =>
  mergeTiming(props.request.state, props.request.execute_after, props.now)
)

const items = computed<KunStepItem[]>(() => {
  const r = props.request
  const closed = r.state === 'rejected' || r.state === 'withdrawn'
  const steps: KunStepItem[] = [
    { title: '提交', description: who(r.proposed_by?.name, r.proposed_at) },
    closed
      ? {
          title: r.state === 'withdrawn' ? '已撤回' : '已驳回',
          description: who(r.decided_by?.name, r.decided_at),
          status: 'error'
        }
      : {
          title: '审核',
          description:
            r.state === 'open'
              ? '等待审核者处理'
              : who(r.decided_by?.name, r.decided_at)
        },
    {
      title: '冷静期',
      description:
        r.state === 'approved'
          ? timing.value.waitMs > 0
            ? `还剩 ${formatWait(timing.value.waitMs)}`
            : '已结束，等待执行'
          : r.execute_after && !closed
            ? `到 ${formatDateTime(r.execute_after)} 结束`
            : ''
    },
    {
      title: '删除',
      description: r.executed_at ? formatDateTime(r.executed_at) : ''
    }
  ]
  if (r.state === 'restored') {
    steps.push({
      title: '已恢复',
      description: who(r.restored_by?.name, r.restored_at)
    })
  }
  return steps
})

const current = computed(() => {
  switch (props.request.state) {
    case 'open':
    case 'rejected':
    case 'withdrawn':
      return 1
    case 'approved':
      return 2
    default:
      return items.value.length
  }
})
</script>

<template>
  <KunSteps
    :items="items"
    :current="current"
    :orientation="props.orientation"
    :color="props.request.state === 'executed' ? 'danger' : 'primary'"
    size="sm"
  />
</template>
