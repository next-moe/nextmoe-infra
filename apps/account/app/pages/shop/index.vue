<script setup lang="ts">
useKunDisableSeo('萌萌点商店')
definePageMeta({
  middleware: ['auth'],
})
</script>

<template>
  <ShopContainer />
</template>
