<script setup lang="ts">
useKunDisableSeo('AI 助手 · 人设')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <AiAgentPersonasContainer />
</template>
