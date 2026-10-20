<script setup lang="ts">
definePageMeta({
  title: '标签详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <TagsDetailContainer />
</template>
