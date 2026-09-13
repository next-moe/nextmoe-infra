<script setup lang="ts">
useKunDisableSeo('T&S 审核 · 队列')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <TrustQueue />
</template>
