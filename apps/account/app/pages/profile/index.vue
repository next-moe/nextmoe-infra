<script setup lang="ts">
useKunDisableSeo('个人资料')
definePageMeta({
  middleware: ['auth'],
})
</script>

<template>
  <ProfileContainer />
</template>
