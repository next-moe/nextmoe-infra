<script setup lang="ts">
useKunDisableSeo('配置中心')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <SettingsContainer />
</template>
