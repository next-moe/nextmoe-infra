<script setup lang="ts">
definePageMeta({
  title: '编辑引擎',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <EnginesEditContainer />
</template>
