<script setup lang="ts">
useHead({ title: '我的删除请求 · 未萌编辑台' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-5">
    <KunHeader
      name="我的删除请求"
      description="待审核的请求可以撤回。审核通过后有一段冷静期，之后才会删除。"
    />
    <DeleteQueue mode="mine" />
  </div>
</template>
