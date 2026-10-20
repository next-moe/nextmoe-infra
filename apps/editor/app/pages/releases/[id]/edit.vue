<script setup lang="ts">
definePageMeta({
  title: '编辑发行',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="release" />
</template>
