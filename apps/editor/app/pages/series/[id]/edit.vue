<script setup lang="ts">
definePageMeta({
  title: '编辑系列',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <SeriesEditContainer />
</template>
