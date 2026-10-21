<script setup lang="ts">
definePageMeta({
  title: '合并请求',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <MergeWorkbenchRoot mode="request" />
</template>
