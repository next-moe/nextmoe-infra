<script setup lang="ts">
useKunDisableSeo('开发者平台 · DLsite 分销')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <DevapiStoreContainer />
</template>
