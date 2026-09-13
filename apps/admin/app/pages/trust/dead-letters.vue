<script setup lang="ts">
useKunDisableSeo('T&S 审核 · 回调死信')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <TrustDeadLetters />
</template>
