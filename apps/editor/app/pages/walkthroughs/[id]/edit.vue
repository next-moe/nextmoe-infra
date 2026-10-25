<script setup lang="ts">
definePageMeta({
  title: '编辑攻略',
  layout: false,
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WalkthroughsEditorContainer mode="edit" />
</template>
