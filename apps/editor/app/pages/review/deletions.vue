<script setup lang="ts">
definePageMeta({
  redirect: (to) => ({
    path: '/review/requests',
    query: { ...to.query, tab: 'delete' }
  })
})
</script>

<template>
  <div />
</template>
