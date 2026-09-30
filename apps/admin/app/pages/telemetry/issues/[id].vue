<script setup lang="ts">
useKunDisableSeo('应用监测 · 问题详情')
definePageMeta({ middleware: ['auth', 'ren'] })
</script>

<template>
  <TelemetryIssuesDetailContainer />
</template>
