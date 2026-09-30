<script setup lang="ts">
useKunDisableSeo('应用监测 · 概览')
definePageMeta({ middleware: ['auth', 'ren'] })
</script>

<template>
  <TelemetryContainer />
</template>
