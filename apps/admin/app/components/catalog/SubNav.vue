<script setup lang="ts">
import {
  CANDIDATE_STATUS,
  CATALOG_QUEUE_SUMMARY_KEY,
  PROPOSAL_STATUS
} from '~/constants/catalog'
import type {
  CatalogProposalPage,
  CatalogQueueSummary
} from '~~/shared/types/catalog'

const route = useRoute()

// The badge used to count status=pending only, which reported 67 while ~6.4k
// work pairs sat in needsManual — the queue the console exists to drain was
// invisible from the nav.
const OPEN_CANDIDATE_STATUSES = [
  CANDIDATE_STATUS.pending,
  CANDIDATE_STATUS.needsManual
] as number[]

const { data: summary } = await useApiFetch<CatalogQueueSummary>(
  '/admin/catalog/candidates/summary',
  { key: CATALOG_QUEUE_SUMMARY_KEY },
  'catalog'
)
const { data: openProposals } = await useApiFetch<CatalogProposalPage>(
  '/admin/catalog/proposals',
  { query: { status: PROPOSAL_STATUS.open, limit: 1 } },
  'catalog'
)

const openCandidates = computed(() =>
  (summary.value?.candidates ?? [])
    .filter((b) => OPEN_CANDIDATE_STATUSES.includes(b.status))
    .reduce((sum, b) => sum + b.count, 0)
)
const probableRefs = computed(() =>
  (summary.value?.probable_refs ?? []).reduce((sum, b) => sum + b.count, 0)
)

const tabs = computed(() => [
  {
    to: '/catalog/candidates',
    label: '候选',
    icon: 'lucide:git-compare',
    count: openCandidates.value
  },
  {
    to: '/catalog/proposals',
    label: '合并提案',
    icon: 'lucide:git-merge',
    count: openProposals.value?.total ?? 0
  },
  {
    to: '/catalog/refs',
    label: '外链确认',
    icon: 'lucide:link',
    count: probableRefs.value
  }
])
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <KunButton
      v-for="tab in tabs"
      :key="tab.to"
      :color="route.path === tab.to ? 'primary' : 'default'"
      :variant="route.path === tab.to ? 'solid' : 'flat'"
      size="sm"
      @click="navigateTo(tab.to)"
    >
      <KunIcon :name="tab.icon" class="mr-1 size-4" />
      {{ tab.label }}
      <KunChip
        v-if="tab.count > 0"
        :color="route.path === tab.to ? 'default' : 'warning'"
        variant="flat"
        size="xs"
        class="ml-2"
      >
        {{ tab.count }}
      </KunChip>
    </KunButton>
  </div>
</template>
