<script setup lang="ts">
definePageMeta({
  validate: (route) => /^\d+$/.test(String(route.params.id)),
  redirect: (to) => `/walkthroughs/${String(to.params.id)}/edit`
})
</script>

<template>
  <div />
</template>
