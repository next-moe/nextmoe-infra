<script setup lang="ts">
const props = defineProps<{ requestId: string }>()
</script>

<template>
  <div
    data-slot="discussion-thread"
    data-object="merge_request"
    :data-id="props.requestId"
  />
</template>
