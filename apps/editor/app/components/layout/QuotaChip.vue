<script setup lang="ts">
import type { EditQuota } from '#shared/types/quota'

const api = useEditorApi()

const { data } = await useAsyncData('editor-quota', () =>
  api<EditQuota>('/v2/me/edit-quota').catch(() => null)
)

const proposals = computed(() => data.value?.proposals ?? null)
</script>

<template>
  <KunTooltip v-if="proposals" text="今日已用提案 / 每日上限" position="bottom">
    <KunChip
      size="sm"
      variant="flat"
      color="default"
      class-name="hidden md:inline-flex"
    >
      <KunIcon name="lucide:gauge" class="size-3.5" />
      {{ proposals.used }} / {{ proposals.limit }}
    </KunChip>
  </KunTooltip>
</template>
