<script setup lang="ts">
definePageMeta({ middleware: ['auth', 'ren'] })

await navigateTo('/catalog/candidates', { redirectCode: 302, replace: true })
</script>

<template>
  <div />
</template>
