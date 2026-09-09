<script setup lang="ts">
useKunDisableSeo('T&S 审核 · Tier0 词表')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <TrustTerms />
</template>
