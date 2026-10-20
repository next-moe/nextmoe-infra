<script setup lang="ts">
definePageMeta({
  title: '编辑标签',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <TagsEditContainer />
</template>
