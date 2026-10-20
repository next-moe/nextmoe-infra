<script setup lang="ts">
const props = withDefaults(defineProps<{ title: string; hint?: string }>(), {
  hint: ''
})
</script>

<template>
  <div class="bg-default-50 border-default-200 space-y-2 border-b px-3 py-3">
    <p class="text-foreground text-xs font-semibold">{{ props.title }}</p>
    <slot />
    <p v-if="props.hint" class="text-default-500 text-xs leading-5">
      {{ props.hint }}
    </p>
  </div>
</template>
