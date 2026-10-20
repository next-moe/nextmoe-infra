<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const isLinks = computed(() => props.fieldKey.endsWith('.links'))
</script>

<template>
  <EditLinksTable v-if="isLinks" :field-key="props.fieldKey" />
  <EditListStrings v-else :field-key="props.fieldKey" />
</template>
