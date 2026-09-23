<script setup lang="ts">
useKunDisableSeo('萌萌点商店')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <ShopContainer />
</template>
