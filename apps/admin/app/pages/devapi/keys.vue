<script setup lang="ts">
useKunDisableSeo('开发者平台 · 全部密钥')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <DevapiKeys />
</template>
