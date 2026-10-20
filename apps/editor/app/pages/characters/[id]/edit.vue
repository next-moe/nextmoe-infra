<script setup lang="ts">
definePageMeta({
  title: '编辑角色',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="character" />
</template>
