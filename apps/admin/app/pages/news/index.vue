<script setup lang="ts">
useKunDisableSeo('情报审核 · 队列')
definePageMeta({ middleware: ['auth', 'news-review'] })
</script>

<template>
  <NewsContainer />
</template>
