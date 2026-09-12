<script setup lang="ts">
useKunDisableSeo('OAuth 客户端')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <OauthClientsContainer />
</template>
