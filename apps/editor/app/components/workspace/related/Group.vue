<script setup lang="ts">
const props = withDefaults(
  defineProps<{ label: string; count?: number | null }>(),
  { count: null }
)
</script>

<template>
  <div
    class="bg-content1 border-default-100 text-default-500 sticky top-0 z-10 flex items-center gap-2 border-b px-3 py-1.5 text-xs font-medium"
  >
    <span class="truncate">{{ props.label }}</span>
    <span v-if="props.count !== null" class="text-default-400 tabular-nums">
      {{ props.count }}
    </span>
  </div>
</template>
