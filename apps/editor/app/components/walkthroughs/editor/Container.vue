<script setup lang="ts">
const props = defineProps<{ mode: 'edit' | 'create' }>()
</script>

<template>
  <WalkthroughsEditorEditSession v-if="props.mode === 'edit'" />
  <WalkthroughsEditorCreateSession v-else />
</template>
