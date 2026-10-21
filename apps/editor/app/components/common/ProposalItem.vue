<script setup lang="ts">
import type { CatalogProposal } from '#shared/types/catalog'
import { PROPOSAL_STATE, SITE_LABELS } from '~/constants/catalog'

const props = defineProps<{
  proposal: CatalogProposal
  name: string
  to?: string
}>()

const { originLabel } = useOriginSites()
const source = computed(() =>
  props.proposal.origin_site
    ? originLabel(props.proposal.origin_site)
    : labelOf(SITE_LABELS, props.proposal.site)
)

const state = computed(
  () =>
    PROPOSAL_STATE[props.proposal.state] ?? {
      label: props.proposal.state,
      color: 'default' as const
    }
)
</script>

<template>
  <li class="flex items-start gap-3 py-3">
    <KunChip
      size="sm"
      variant="flat"
      :color="state.color"
      class-name="mt-0.5 shrink-0"
    >
      {{ state.label }}
    </KunChip>
    <div class="min-w-0 flex-1">
      <CommonEntityLink
        :family="props.proposal.target_object"
        :id="props.proposal.entity_id"
        :name="props.name"
      />
      <p
        v-if="props.proposal.note"
        class="text-default-500 mt-0.5 line-clamp-2 text-sm"
      >
        {{ props.proposal.note }}
      </p>
      <p class="text-default-400 mt-1 text-xs">
        <NuxtLink v-if="props.to" :to="props.to" class="hover:text-primary">
          提案 #{{ props.proposal.id }}
        </NuxtLink>
        <template v-else>提案 #{{ props.proposal.id }}</template>
        · 来自 {{ source }} ·
        <time
          :datetime="props.proposal.created_at"
          :title="formatDateTime(props.proposal.created_at)"
        >
          {{ formatRelative(props.proposal.created_at) }}
        </time>
      </p>
    </div>
  </li>
</template>
