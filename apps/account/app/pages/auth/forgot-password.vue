<script setup lang="ts">
useKunSeoMeta({
  title: '忘记密码',
  description: '重置 NextMoe·未萌 账号密码'
})
definePageMeta({
  layout: 'auth',
})
</script>

<template>
  <AuthForgotPasswordForm />
</template>
