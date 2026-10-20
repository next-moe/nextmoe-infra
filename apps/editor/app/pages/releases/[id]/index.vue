<script setup lang="ts">
definePageMeta({
  title: '发行详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <ReleasesDetailContainer />
</template>
