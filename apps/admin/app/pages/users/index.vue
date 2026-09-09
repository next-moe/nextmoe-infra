<script setup lang="ts">
useKunDisableSeo('用户管理')
definePageMeta({
  middleware: ['auth', 'admin'],
})
</script>

<template>
  <UsersContainer />
</template>
