<script setup lang="ts">
useKunDisableSeo('环境变量与密钥')
definePageMeta({ middleware: ['auth', 'admin'] })
</script>

<template>
  <SettingsEnvContainer />
</template>
