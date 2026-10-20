<script setup lang="ts">
import type { RelatedState } from '~/utils/relatedRows'

const props = withDefaults(
  defineProps<{
    title?: string
    origin?: 'human' | 'upstream' | null
    source?: string
    state?: RelatedState
    note?: string
  }>(),
  {
    title: '在本作中',
    origin: null,
    source: '',
    state: null,
    note: ''
  }
)
</script>

<template>
  <section class="space-y-3">
    <header class="flex flex-wrap items-center gap-2">
      <h3 class="text-sm font-semibold">{{ props.title }}</h3>
      <WorkspaceSourceBadge
        v-if="props.origin"
        :source="props.source"
        :human="props.origin === 'human'"
      />
      <KunChip
        v-if="props.state === 'added'"
        size="xs"
        variant="flat"
        color="success"
      >
        新增
      </KunChip>
      <KunChip
        v-else-if="props.state === 'changed'"
        size="xs"
        variant="flat"
        color="primary"
      >
        已修改
      </KunChip>
      <KunChip
        v-else-if="props.state === 'hidden'"
        size="xs"
        variant="flat"
        color="default"
      >
        已隐藏
      </KunChip>
      <span class="flex-1" />
      <slot name="actions" />
    </header>
    <slot />
    <p v-if="props.note" class="text-default-500 text-xs leading-5">
      {{ props.note }}
    </p>
  </section>
</template>
