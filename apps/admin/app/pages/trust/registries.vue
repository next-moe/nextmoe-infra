<script setup lang="ts">
useKunDisableSeo('T&S 审核 · 注册表')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <TrustRegistries />
</template>
