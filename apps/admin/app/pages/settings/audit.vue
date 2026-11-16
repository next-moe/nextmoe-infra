<script setup lang="ts">
useKunDisableSeo('配置变更记录')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <SettingsAuditContainer />
</template>
