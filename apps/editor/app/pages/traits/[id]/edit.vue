<script setup lang="ts">
definePageMeta({
  title: '编辑特质',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <TraitsEditContainer />
</template>
