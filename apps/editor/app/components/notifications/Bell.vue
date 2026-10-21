<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { ProposalNotice, ProposalNoticeKind } from '#shared/types/activity'
import type { DiscussionNotice } from '#shared/types/discussion'
import {
  DISCUSSION_ANCHOR_LABELS,
  discussionAnchorRoute
} from '~/constants/discussion'

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

const { notices, discussionNotices, refresh, markAllSeen, startPolling } =
  useProposalNotices()
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

type BellEntry =
  | { type: 'proposal'; key: string; at: string; notice: ProposalNotice }
  | { type: 'discussion'; key: string; at: string; notice: DiscussionNotice }

const entries = computed<BellEntry[]>(() =>
  [
    ...notices.value.map((notice) => ({
      type: 'proposal' as const,
      key: `proposal-${notice.proposal.id}-${notice.kind}`,
      at: notice.at,
      notice
    })),
    ...discussionNotices.value.map((notice) => ({
      type: 'discussion' as const,
      key: `discussion-${notice.post.id}`,
      at: notice.post.created_at,
      notice
    }))
  ].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
)

const count = computed(() => entries.value.length)

const discussionTitle = (notice: DiscussionNotice) => {
  const who = notice.post.author?.name ?? `用户 #${notice.post.author_uid}`
  const where = `${DISCUSSION_ANCHOR_LABELS[notice.post.anchor_kind] ?? '请求'} #${notice.post.anchor_id}`
  return notice.reason === 'mention'
    ? `${who} 在${where}提到了你`
    : `${who} 在${where}留言`
}

const discussionLink = (notice: DiscussionNotice) =>
  discussionAnchorRoute(
    notice.post.anchor_kind,
    notice.post.anchor_id,
    notice.viewer_is_owner
  ) ?? '/me/proposals'
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
        <p class="text-sm font-medium">通知</p>
        <KunButton v-if="count" variant="light" size="xs" @click="markAllSeen">
          全部已读
        </KunButton>
      </div>

      <ul v-if="count" class="max-h-96 overflow-y-auto py-1">
        <li v-for="entry in entries" :key="entry.key">
          <NuxtLink
            v-if="entry.type === 'proposal'"
            :to="`/me/proposals/${entry.notice.proposal.id}`"
            class="hover:bg-default-100 flex items-start gap-3 px-3 py-2 transition-colors"
          >
            <span
              :class="
                cn(
                  'mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full',
                  NOTICE_COPY[entry.notice.kind].tone
                )
              "
            >
              <KunIcon
                :name="NOTICE_COPY[entry.notice.kind].icon"
                class="size-3.5"
              />
            </span>
            <span class="min-w-0 flex-1 text-sm">
              <span class="block truncate font-medium">
                {{ names.nameOf(entry.notice.proposal) }}
              </span>
              <span class="text-default-500 block text-xs">
                提案 #{{ entry.notice.proposal.id }}
                {{ NOTICE_COPY[entry.notice.kind].label }}
                <template v-if="entry.notice.proposal.decided_by?.name">
                  · {{ entry.notice.proposal.decided_by.name }}
                </template>
              </span>
              <time
                :datetime="entry.at"
                :title="formatDateTime(entry.at)"
                class="text-default-400 text-xs"
              >
                {{ formatRelative(entry.at) }}
              </time>
            </span>
          </NuxtLink>
          <NuxtLink
            v-else
            :to="discussionLink(entry.notice)"
            class="hover:bg-default-100 flex items-start gap-3 px-3 py-2 transition-colors"
          >
            <span
              :class="
                cn(
                  'mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full',
                  entry.notice.reason === 'mention'
                    ? 'bg-secondary-50 text-secondary-600'
                    : 'bg-info-50 text-info-600'
                )
              "
            >
              <KunIcon
                :name="
                  entry.notice.reason === 'mention'
                    ? 'lucide:at-sign'
                    : 'lucide:message-circle'
                "
                class="size-3.5"
              />
            </span>
            <span class="min-w-0 flex-1 text-sm">
              <span class="block truncate font-medium">
                {{ discussionTitle(entry.notice) }}
              </span>
              <span class="text-default-500 line-clamp-2 block text-xs">
                {{ discussionSnippet(entry.notice.post.body, 80) }}
              </span>
              <time
                :datetime="entry.at"
                :title="formatDateTime(entry.at)"
                class="text-default-400 text-xs"
              >
                {{ formatRelative(entry.at) }}
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
