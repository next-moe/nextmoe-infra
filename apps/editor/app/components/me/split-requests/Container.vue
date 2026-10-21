<script setup lang="ts">
useHead({ title: '我的拆分请求 · 未萌编辑台' })
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <KunHeader
      name="我的拆分请求"
      description="把一个版本拆成单独的作品需要审核；批准后先过冷静期再执行，执行后一段时间内审核者可以撤销。"
    />
    <SplitQueue mode="mine" />
  </div>
</template>
