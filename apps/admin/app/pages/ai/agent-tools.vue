<script setup lang="ts">
useKunDisableSeo('AI 助手 · 站点工具')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <AiAgentToolsContainer />
</template>
