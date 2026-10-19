<script setup lang="ts">
import type { CatalogProposal } from '#shared/types/catalog'
import { PROPOSAL_STATE, SITE_LABELS } from '~/constants/catalog'

const props = defineProps<{
  proposal: CatalogProposal
  name: string
  conflictCount: number
}>()

const state = computed(
  () =>
    PROPOSAL_STATE[props.proposal.state] ?? {
      label: props.proposal.state,
      color: 'default' as const
    }
)
</script>

<template>
  <KunCard bordered padding="md" content-class="space-y-2">
    <div class="flex flex-wrap items-center gap-2">
      <h1 class="sr-only">审核 {{ props.name }}</h1>
      <div class="text-foreground text-lg font-semibold">
        <CommonEntityLink
          :family="props.proposal.target_object"
          :id="props.proposal.entity_id"
          :name="props.name"
        />
      </div>
      <KunChip size="sm" variant="flat" :color="state.color">{{
        state.label
      }}</KunChip>
      <KunChip
        v-if="props.conflictCount"
        size="sm"
        variant="flat"
        color="warning"
      >
        {{ props.conflictCount }} 个字段在提案之后被改过
      </KunChip>
      <NuxtLink
        :to="`/history/${props.proposal.target_object}/${props.proposal.entity_id}`"
        class="text-primary ml-auto inline-flex items-center gap-1 text-sm"
      >
        <KunIcon name="lucide:history" class="size-4" />
        修订历史
      </NuxtLink>
    </div>
    <p class="text-default-500 flex flex-wrap items-center gap-x-1 text-sm">
      <span>提案 #{{ props.proposal.id }} ·</span>
      <HistoryUser :uid="props.proposal.proposer_uid" />
      <span
        >· 来自 {{ labelOf(SITE_LABELS, props.proposal.site) }} · 基于第
        {{ props.proposal.base_revision_seq }} 版 ·</span
      >
      <time
        :datetime="props.proposal.created_at"
        :title="formatDateTime(props.proposal.created_at)"
        >{{ formatRelative(props.proposal.created_at) }}</time
      >
    </p>
    <p
      v-if="props.proposal.note"
      class="bg-default-50 text-foreground rounded-md px-3 py-2 text-sm whitespace-pre-wrap"
    >
      {{ props.proposal.note }}
    </p>
  </KunCard>
</template>
