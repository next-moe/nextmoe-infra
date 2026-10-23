<script setup lang="ts">
useHead({ title: '页面不存在 · 未萌编辑台' })
</script>

<template>
  <div
    class="mx-auto flex max-w-xl flex-col items-center gap-4 py-16 text-center"
  >
    <KunNull description="没有找到这个页面" />
    <KunButton variant="bordered" @click="navigateTo('/')"
      >回到工作台</KunButton
    >
  </div>
</template>
