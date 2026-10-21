<script setup lang="ts">
definePageMeta({
  title: '拆分请求',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <SplitWorkbenchRoot mode="request" />
</template>
