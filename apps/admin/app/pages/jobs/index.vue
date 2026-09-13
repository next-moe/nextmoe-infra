<script setup lang="ts">
useKunDisableSeo('后台任务')
definePageMeta({
  middleware: ['auth', 'admin']
})
</script>

<template>
  <JobsContainer />
</template>
