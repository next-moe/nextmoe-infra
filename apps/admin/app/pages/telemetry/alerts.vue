<script setup lang="ts">
useKunDisableSeo('应用监测 · 告警')
definePageMeta({ middleware: ['auth', 'ren'] })
</script>

<template>
  <TelemetryAlertsContainer />
</template>
