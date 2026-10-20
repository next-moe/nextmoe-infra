<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    text?: string
    position?: 'top' | 'bottom' | 'left' | 'right'
  }>(),
  { text: '', position: 'top' }
)
</script>

<template>
  <KunTooltip v-if="props.text" :text="props.text" :position="props.position">
    <slot />
  </KunTooltip>
  <slot v-else />
</template>
