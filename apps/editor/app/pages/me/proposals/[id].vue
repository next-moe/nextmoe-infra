<script setup lang="ts">
definePageMeta({
  title: '提案详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <MeProposalsDetail />
</template>
