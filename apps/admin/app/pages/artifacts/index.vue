<script setup lang="ts">
useKunDisableSeo('用量概览')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <ArtifactsDashboard />
</template>
