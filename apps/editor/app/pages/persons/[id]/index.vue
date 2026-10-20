<script setup lang="ts">
definePageMeta({
  title: '人物详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <PersonsDetailContainer />
</template>
