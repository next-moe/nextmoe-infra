<script setup lang="ts">
definePageMeta({
  title: '署名名详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <CreditNamesDetailContainer />
</template>
