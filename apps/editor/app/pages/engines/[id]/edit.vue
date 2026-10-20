<script setup lang="ts">
definePageMeta({
  title: '编辑引擎',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="engine" />
</template>
