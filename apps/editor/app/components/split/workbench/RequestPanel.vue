<script setup lang="ts">
import { SPLIT_MOVED_LABELS } from '~/constants/split'
import { splitTimeline } from '~/utils/split/timeline'

const props = withDefaults(defineProps<{ compact?: boolean }>(), {
  compact: false
})

const { request } = useSplitWorkbench()

const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  ticker = setInterval(() => (now.value = Date.now()), 30_000)
})
onBeforeUnmount(() => clearInterval(ticker))

const steps = computed(() =>
  request.value ? splitTimeline(request.value, now.value) : []
)
const moved = computed(() =>
  (request.value?.moved ?? []).filter((m) => m.count > 0)
)
</script>

<template>
  <div v-if="request" class="space-y-4">
    <KunCard bordered padding="md">
      <MergeWorkbenchTimeline :steps="steps" :now="now" />
    </KunCard>

    <KunCard bordered padding="md" class-name="space-y-3">
      <div class="text-default-500 flex flex-wrap items-center gap-1.5 text-sm">
        <HistoryAuthor
          :user="request.proposed_by"
          :uid="request.proposed_by_uid"
          :origin="request.origin_site"
          size="sm"
        />
        <span class="text-default-400">
          提交于 {{ formatDateTime(request.proposed_at) }}
        </span>
      </div>
      <p
        class="bg-default-50 text-default-700 rounded-lg p-3 text-sm whitespace-pre-line"
      >
        {{ request.note }}
      </p>
      <p
        v-if="request.amended_by_uid"
        class="text-default-500 flex items-center gap-1.5 text-xs"
      >
        <KunIcon name="lucide:pencil-line" class="size-3.5" />
        审核者 {{ request.amended_by?.name ?? `#${request.amended_by_uid}` }} 于
        {{ formatDateTime(request.amended_at ?? '') }}
        修改过所选发行或新作品资料
      </p>
      <KunInfo
        v-if="request.decision_note"
        :color="request.state === 'rejected' ? 'danger' : 'default'"
        variant="flat"
        :title="request.decided_by_uid ? '审核意见' : '系统说明'"
        :description="request.decision_note"
      />
      <KunInfo
        v-if="request.undo_note"
        color="secondary"
        variant="flat"
        title="撤销理由"
        :description="request.undo_note"
      />
    </KunCard>

    <KunCard v-if="moved.length && !props.compact" bordered padding="md">
      <h3 class="mb-2 text-sm font-medium">执行时的改动</h3>
      <ul class="grid gap-x-6 gap-y-1.5 sm:grid-cols-2">
        <li
          v-for="m in moved"
          :key="m.key"
          class="flex items-center justify-between gap-3 text-sm"
        >
          <span class="text-default-600">
            {{ SPLIT_MOVED_LABELS[m.key] ?? m.key }}
          </span>
          <span class="tabular-nums">{{ m.count }}</span>
        </li>
      </ul>
    </KunCard>
  </div>
</template>
