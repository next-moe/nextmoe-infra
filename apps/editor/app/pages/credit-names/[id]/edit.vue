<script setup lang="ts">
definePageMeta({
  title: '编辑署名名',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <CreditNamesEditContainer />
</template>
