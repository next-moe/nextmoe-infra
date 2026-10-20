<script setup lang="ts">
definePageMeta({
  title: '编辑人物',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <PersonsEditContainer />
</template>
