<script setup lang="ts">
useKunDisableSeo('应用监测 · 应用与密钥')
definePageMeta({ middleware: ['auth', 'ren'] })
</script>

<template>
  <TelemetryAppsContainer />
</template>
