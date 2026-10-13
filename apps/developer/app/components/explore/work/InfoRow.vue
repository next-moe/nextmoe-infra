<script setup lang="ts">
defineProps<{ label: string }>()
</script>

<template>
  <div class="grid grid-cols-[5rem_minmax(0,1fr)] gap-3 px-4 py-2.5">
    <dt class="text-default-400">{{ label }}</dt>
    <dd class="text-foreground min-w-0 space-y-0.5 break-words">
      <slot />
    </dd>
  </div>
</template>
