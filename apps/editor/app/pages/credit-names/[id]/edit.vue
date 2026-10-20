<script setup lang="ts">
definePageMeta({
  title: '编辑署名名',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="credit_name" />
</template>
