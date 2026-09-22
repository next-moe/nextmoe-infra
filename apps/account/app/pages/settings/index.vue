<script setup lang="ts">
useKunDisableSeo('设置')
definePageMeta({
  middleware: ['auth']
})
</script>

<template>
  <SettingsContainer />
</template>
