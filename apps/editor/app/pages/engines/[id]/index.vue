<script setup lang="ts">
definePageMeta({
  title: '引擎详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <EnginesDetailContainer />
</template>
