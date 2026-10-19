<script setup lang="ts">
const props = defineProps<{ title: string; count?: number }>()
</script>

<template>
  <KunCard bordered padding="md">
    <div class="mb-3 flex items-baseline gap-2">
      <h2 class="font-semibold">{{ props.title }}</h2>
      <span
        v-if="props.count !== undefined"
        class="text-default-400 text-sm tabular-nums"
      >
        {{ props.count }}
      </span>
    </div>
    <slot />
  </KunCard>
</template>
