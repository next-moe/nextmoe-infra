<script setup lang="ts">
import type { CatalogProposalList } from '#shared/types/catalog'

const api = useEditorApi()

const { data } = await useAsyncData('editor-review-count', () =>
  api<CatalogProposalList>('/v2/moderation/proposals', {
    query: { limit: 1, include_total: true }
  }).catch(() => null)
)

const pending = computed(() => data.value?.total ?? 0)
</script>

<template>
  <KunTooltip :text="`待审提案 ${pending}`" position="bottom">
    <KunBadge :count="pending" :max="99" color="danger" size="sm">
      <KunButton
        variant="light"
        size="sm"
        is-icon-only
        aria-label="待审队列"
        @click="navigateTo('/review')"
      >
        <KunIcon name="lucide:inbox" class="size-5" />
      </KunButton>
    </KunBadge>
  </KunTooltip>
</template>
