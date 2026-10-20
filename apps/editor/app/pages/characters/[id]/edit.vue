<script setup lang="ts">
definePageMeta({
  title: '编辑角色',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <CharactersEditContainer />
</template>
