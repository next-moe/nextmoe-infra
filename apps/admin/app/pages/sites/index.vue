<script setup lang="ts">
useKunDisableSeo('站点管理')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <SitesContainer />
</template>
