<script setup lang="ts">
import { mediaRowChanges, type MediaRow } from '~/utils/mediaGallery'

const props = defineProps<{ before: MediaRow | null; after: MediaRow }>()

const changes = computed(() => mediaRowChanges(props.before, props.after))
</script>

<template>
  <ul class="space-y-0.5 text-sm">
    <li
      v-for="change in changes"
      :key="change.label"
      class="flex flex-wrap items-center gap-1.5"
    >
      <span class="text-default-500">{{ change.label }}</span>
      <span class="text-default-400 line-through">{{ change.before }}</span>
      <KunIcon name="lucide:arrow-right" class="text-default-400 size-3" />
      <span class="font-medium">{{ change.after }}</span>
    </li>
  </ul>
</template>
