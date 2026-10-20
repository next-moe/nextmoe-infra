<script setup lang="ts">
definePageMeta({
  title: '编辑特质',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="trait" />
</template>
