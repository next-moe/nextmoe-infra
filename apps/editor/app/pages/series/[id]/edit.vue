<script setup lang="ts">
definePageMeta({
  title: '编辑系列',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="series" />
</template>
