<script setup lang="ts">
useKunDisableSeo('开发者平台')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <DevapiContainer />
</template>
