<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogProposal } from '#shared/types/catalog'
import { ENTITY_FAMILY, SITE_LABELS } from '~/constants/catalog'
import type { QueueAnalysis } from '~/utils/review/queue'

const props = defineProps<{
  proposal: CatalogProposal
  name: string
  analysis?: QueueAnalysis
  selected?: boolean
}>()

const familyLabel = computed(
  () =>
    ENTITY_FAMILY[props.proposal.target_object]?.label ??
    props.proposal.target_object
)

const conflictCount = computed(
  () => Object.keys(props.analysis?.conflicts ?? {}).length
)
</script>

<template>
  <li>
    <NuxtLink
      :to="`/review/${props.proposal.id}`"
      :class="
        cn(
          'flex items-start gap-3 px-4 py-3 transition-colors',
          props.selected ? 'bg-primary-50' : 'hover:bg-default-50'
        )
      "
    >
      <KunChip size="sm" variant="flat" class-name="mt-0.5 shrink-0">
        {{ familyLabel }}
      </KunChip>
      <div class="min-w-0 flex-1">
        <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span class="text-foreground truncate font-medium">{{
            props.name
          }}</span>
          <span class="text-default-400 text-xs"
            >提案 #{{ props.proposal.id }}</span
          >
          <KunChip
            v-if="conflictCount"
            size="xs"
            color="warning"
            variant="flat"
          >
            {{ conflictCount }} 个字段可能冲突
          </KunChip>
        </div>
        <p
          v-if="props.proposal.note"
          class="text-default-500 mt-0.5 line-clamp-2 text-sm"
        >
          {{ props.proposal.note }}
        </p>
        <div
          v-if="props.analysis?.fields.length"
          class="mt-1 flex flex-wrap gap-1"
        >
          <KunChip
            v-for="field in props.analysis.fields"
            :key="field"
            size="xs"
            variant="flat"
          >
            {{ fieldLabel(field) }}
          </KunChip>
        </div>
        <p
          class="text-default-400 mt-1 flex flex-wrap items-center gap-x-1 text-xs"
        >
          <HistoryUser
            :user="props.proposal.proposer"
            :uid="props.proposal.proposer_uid"
          />
          <span>· {{ labelOf(SITE_LABELS, props.proposal.site) }} ·</span>
          <time
            :datetime="props.proposal.created_at"
            :title="formatDateTime(props.proposal.created_at)"
          >
            {{ formatRelative(props.proposal.created_at) }}
          </time>
        </p>
      </div>
      <KunIcon
        name="lucide:chevron-right"
        class="text-default-300 mt-1 size-4 shrink-0"
      />
    </NuxtLink>
  </li>
</template>
