<script setup lang="ts">
const props = defineProps<{ label?: string }>()
</script>

<template>
  <button
    type="button"
    class="text-default-300 hover:text-default-500 focus-visible:ring-primary-300 shrink-0 cursor-grab rounded p-0.5 focus-visible:ring-2 focus-visible:outline-none"
    :aria-label="props.label ?? '拖动排序，或按 Alt + ↑/↓ 移动'"
  >
    <KunIcon name="lucide:grip-vertical" class="size-4" />
  </button>
</template>
