<script setup lang="ts">
useKunDisableSeo('T&S 审核 · 站点策略')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <TrustSitePolicies />
</template>
