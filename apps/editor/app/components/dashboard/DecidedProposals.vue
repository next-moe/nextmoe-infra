<script setup lang="ts">
import type { ActivityList } from '#shared/types/activity'
import type { CatalogProposal } from '#shared/types/catalog'
import { PROPOSAL_STATE } from '~/constants/catalog'

const SHOWN = 6

const api = useEditorApi()
const names = useEntityNames()

const fetchState = (state: 'merged' | 'declined') =>
  api<ActivityList<CatalogProposal>>('/v2/me/proposals', {
    query: { state, limit: SHOWN }
  })
    .then((list) => list.items ?? [])
    .catch(() => [] as CatalogProposal[])

const { data, pending } = useLazyAsyncData(
  'dashboard-decided-proposals',
  async () => {
    const [merged, declined] = await Promise.all([
      fetchState('merged'),
      fetchState('declined')
    ])
    return [...merged, ...declined]
      .filter((proposal) => proposal.decided_at)
      .sort(
        (a, b) =>
          Date.parse(b.decided_at ?? '') - Date.parse(a.decided_at ?? '')
      )
      .slice(0, SHOWN)
  }
)

watch(data, (list) => names.resolve(list ?? []), { immediate: true })
</script>

<template>
  <KunCard bordered padding="md">
    <div class="mb-2 flex items-center justify-between">
      <h2 class="font-semibold">最近处理的提案</h2>
      <NuxtLink to="/me/proposals" class="text-primary text-sm">全部</NuxtLink>
    </div>
    <CommonSkeletonRows v-if="pending && !data" :rows="4" />
    <ul v-else-if="data?.length" class="divide-default-200 divide-y">
      <li
        v-for="proposal in data"
        :key="proposal.id"
        class="flex items-start gap-3 py-3"
      >
        <KunChip
          size="sm"
          variant="flat"
          :color="PROPOSAL_STATE[proposal.state]?.color ?? 'default'"
          class-name="mt-0.5 shrink-0"
        >
          {{ PROPOSAL_STATE[proposal.state]?.label ?? proposal.state }}
        </KunChip>
        <div class="min-w-0 flex-1">
          <CommonEntityLink
            :family="proposal.target_object"
            :id="proposal.entity_id"
            :name="names.nameOf(proposal)"
          />
          <p class="text-default-400 mt-1 text-xs">
            <NuxtLink
              :to="`/me/proposals/${proposal.id}`"
              class="hover:text-primary"
            >
              提案 #{{ proposal.id }}
            </NuxtLink>
            <template v-if="proposal.decided_by?.name">
              · 由 {{ proposal.decided_by.name }} 处理
            </template>
            ·
            <time
              :datetime="proposal.decided_at ?? undefined"
              :title="formatDateTime(proposal.decided_at)"
            >
              {{ formatRelative(proposal.decided_at) }}
            </time>
          </p>
        </div>
      </li>
    </ul>
    <KunNull v-else description="还没有被处理的提案" :is-show-sticker="false" />
  </KunCard>
</template>
