<script setup lang="ts">
useKunDisableSeo('开发者平台 · 应用详情')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <DevapiDetail />
</template>
