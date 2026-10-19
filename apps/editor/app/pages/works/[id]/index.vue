<script setup lang="ts">
definePageMeta({
  title: '作品详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorksDetailContainer />
</template>
