<script setup lang="ts">
definePageMeta({
  title: '厂商详情',
  validate: (route) => /^\d+$/.test(String(route.params.id))
})
</script>

<template>
  <CompaniesDetailContainer />
</template>
