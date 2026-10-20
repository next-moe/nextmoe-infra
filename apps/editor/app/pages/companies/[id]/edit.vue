<script setup lang="ts">
definePageMeta({
  title: '编辑厂商',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorkspaceRoot family="company" />
</template>
