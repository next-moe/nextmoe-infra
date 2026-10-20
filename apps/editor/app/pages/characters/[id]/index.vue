<script setup lang="ts">
definePageMeta({
  title: '角色详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <CharactersDetailContainer />
</template>
