<script setup lang="ts">
useKunDisableSeo('注销账号')
definePageMeta({
  middleware: ['auth'],
})
</script>

<template>
  <DeleteAccountContainer />
</template>
