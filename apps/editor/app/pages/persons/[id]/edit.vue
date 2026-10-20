<script setup lang="ts">
definePageMeta({
  title: '编辑人物',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="person" />
</template>
