<script setup lang="ts">
useKunDisableSeo('应用监测 · 问题')
definePageMeta({ middleware: ['auth', 'ren'] })
</script>

<template>
  <TelemetryIssuesContainer />
</template>
