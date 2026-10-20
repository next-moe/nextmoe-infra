<script setup lang="ts">
definePageMeta({
  title: '编辑作品',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="work" />
</template>
