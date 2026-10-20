<script setup lang="ts">
definePageMeta({
  title: '系列详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <SeriesDetailContainer />
</template>
