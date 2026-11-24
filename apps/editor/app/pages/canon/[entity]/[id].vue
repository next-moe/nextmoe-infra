<script setup lang="ts">
definePageMeta({
  title: '自有目录条目',
  validate: (route) =>
    ['work', 'release', 'person', 'org'].includes(String(route.params.entity)) &&
    /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <CanonRecordContainer />
</template>
