<script setup lang="ts">
import type {
  CatalogProposal,
  CatalogProposalList
} from '#shared/types/catalog'

const props = defineProps<{
  proposal: CatalogProposal
}>()

const api = useEditorApi()

const countOf = (state?: string) =>
  api<CatalogProposalList>('/v2/catalog/proposals', {
    query: {
      proposer_uid: props.proposal.proposer_uid,
      include_total: true,
      limit: 1,
      ...(state ? { state } : {})
    }
  })
    .then((list) => list.total ?? null)
    .catch(() => null)

const { data: stats } = await useAsyncData(
  () => `review-proposer-${props.proposal.proposer_uid}`,
  async () => {
    const [all, merged, declined] = await Promise.all([
      countOf(),
      countOf('merged'),
      countOf('declined')
    ])
    return { all, merged, declined }
  }
)

const amendments = computed(() => props.proposal.amendments ?? [])
</script>

<template>
  <KunCard bordered padding="md" content-class="space-y-3">
    <div>
      <p class="text-default-400 mb-1 text-xs">提案人</p>
      <HistoryUser :uid="props.proposal.proposer_uid" />
      <p v-if="stats" class="text-default-500 mt-1 text-xs">
        共 {{ stats.all ?? '—' }} 条提案 · 已合并 {{ stats.merged ?? '—' }} ·
        被拒绝
        {{ stats.declined ?? '—' }}
      </p>
    </div>
    <KunDivider />
    <div>
      <p class="text-default-400 mb-2 text-xs">修正记录</p>
      <KunTimeline v-if="amendments.length">
        <KunTimelineItem
          v-for="amendment in amendments"
          :key="amendment.id"
          :title="`第 ${amendment.seq} 次修正`"
          :time="formatDateTime(amendment.created_at)"
          icon="lucide:pencil"
        >
          <HistoryUser :uid="amendment.amender_uid" />
          <p v-if="amendment.note" class="text-default-500 mt-0.5 text-xs">
            {{ amendment.note }}
          </p>
        </KunTimelineItem>
      </KunTimeline>
      <p v-else class="text-default-400 text-xs">还没有人修正过这条提案</p>
    </div>
  </KunCard>
</template>
