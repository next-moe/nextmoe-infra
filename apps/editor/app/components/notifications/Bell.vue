<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { ProposalNoticeKind } from '#shared/types/activity'

const NOTICE_COPY: Record<
  ProposalNoticeKind,
  { label: string; icon: string; tone: string }
> = {
  merged: {
    label: '已合并',
    icon: 'lucide:git-merge',
    tone: 'bg-success-50 text-success-600'
  },
  declined: {
    label: '已驳回',
    icon: 'lucide:circle-x',
    tone: 'bg-danger-50 text-danger-600'
  },
  updated: {
    label: '有更新',
    icon: 'lucide:pencil',
    tone: 'bg-warning-50 text-warning-600'
  }
}

const { notices, refresh, markAllSeen, startPolling } = useProposalNotices()
const names = useEntityNames()

watch(notices, (list) => names.resolve(list.map((notice) => notice.proposal)), {
  immediate: true
})

let stop: (() => void) | null = null
onMounted(() => {
  refresh()
  stop = startPolling()
})
onBeforeUnmount(() => stop?.())

const count = computed(() => notices.value.length)
</script>

<template>
  <KunPopover position="bottom-end" aria-label="通知" inner-class="w-80">
    <template #trigger>
      <KunBadge :count="count" :max="99" color="danger" size="sm">
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          :aria-label="count ? `通知（${count} 条未读）` : '通知'"
        >
          <KunIcon name="lucide:bell" class="size-5" />
        </KunButton>
      </KunBadge>
    </template>

    <div class="w-80">
      <div
        class="border-default-200 flex items-center justify-between border-b px-3 py-2"
      >
        <p class="text-sm font-medium">提案动态</p>
        <KunButton v-if="count" variant="light" size="xs" @click="markAllSeen">
          全部已读
        </KunButton>
      </div>

      <ul v-if="count" class="max-h-96 overflow-y-auto py-1">
        <li
          v-for="notice in notices"
          :key="`${notice.proposal.id}-${notice.kind}`"
        >
          <NuxtLink
            :to="`/me/proposals/${notice.proposal.id}`"
            class="hover:bg-default-100 flex items-start gap-3 px-3 py-2 transition-colors"
          >
            <span
              :class="
                cn(
                  'mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full',
                  NOTICE_COPY[notice.kind].tone
                )
              "
            >
              <KunIcon :name="NOTICE_COPY[notice.kind].icon" class="size-3.5" />
            </span>
            <span class="min-w-0 flex-1 text-sm">
              <span class="block truncate font-medium">
                {{ names.nameOf(notice.proposal) }}
              </span>
              <span class="text-default-500 block text-xs">
                提案 #{{ notice.proposal.id }}
                {{ NOTICE_COPY[notice.kind].label }}
                <template v-if="notice.proposal.decided_by?.name">
                  · {{ notice.proposal.decided_by.name }}
                </template>
              </span>
              <time
                :datetime="notice.at"
                :title="formatDateTime(notice.at)"
                class="text-default-400 text-xs"
              >
                {{ formatRelative(notice.at) }}
              </time>
            </span>
          </NuxtLink>
        </li>
      </ul>
      <p v-else class="text-default-400 px-3 py-6 text-center text-sm">
        暂无新动态
      </p>

      <div class="border-default-200 border-t px-3 py-2 text-right">
        <NuxtLink to="/me/proposals" class="text-primary text-xs">
          查看全部提案
        </NuxtLink>
      </div>
    </div>
  </KunPopover>
</template>
