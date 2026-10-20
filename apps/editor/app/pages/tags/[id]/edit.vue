<script setup lang="ts">
definePageMeta({
  title: '编辑标签',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="tag" />
</template>
