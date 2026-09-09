<script setup lang="ts">
useKunDisableSeo('图片管理')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <ImagesContainer />
</template>
