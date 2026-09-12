<script setup lang="ts">
useKunDisableSeo('存储配置')
definePageMeta({ middleware: ['auth', 'ren'] })
</script>

<template>
  <ArtifactsConfig />
</template>
