<script setup lang="ts">
useKunDisableSeo('控制台')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <DashboardContainer />
</template>
