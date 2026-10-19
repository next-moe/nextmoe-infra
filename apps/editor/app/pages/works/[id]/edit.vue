<script setup lang="ts">
definePageMeta({
  title: '编辑作品',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <WorksEditContainer />
</template>
