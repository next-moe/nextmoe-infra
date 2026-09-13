<script setup lang="ts">
useKunDisableSeo('创作者申请')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <CreatorApplicationsContainer />
</template>
