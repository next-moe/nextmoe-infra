<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const kind = computed(() =>
  props.fieldKey.endsWith('screenshots') ? 'screenshot' : 'cover'
)
</script>

<template>
  <div class="h-[40rem] overflow-hidden rounded-lg">
    <EditMediaGallery :kind="kind" />
  </div>
</template>
