<script setup lang="ts">
definePageMeta({
  title: '特质详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <TraitsDetailContainer />
</template>
