<script setup lang="ts">
definePageMeta({
  title: '新建攻略',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.query.work_id ?? ''))
})
</script>

<template>
  <WalkthroughsEditorContainer mode="create" />
</template>
