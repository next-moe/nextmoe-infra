<script setup lang="ts">
useKunDisableSeo('文件列表')
definePageMeta({ middleware: ['auth', 'ren'] })
</script>

<template>
  <ArtifactsList />
</template>
