<script setup lang="ts">
definePageMeta({
  title: '编辑厂商',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <CompaniesEditContainer />
</template>
