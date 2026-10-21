<script setup lang="ts">
useHead({ title: '删除请求审核 · 未萌编辑台' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
    >
      <KunHeader
        name="删除请求审核"
        description="批准后进入冷静期，期满后可以立即删除，系统也会每小时执行到期的请求。删除后可以随时恢复。"
      />
      <KunButton href="/review/merges" variant="light" size="sm">
        <KunIcon name="lucide:git-merge" class="size-4" />
        合并审核
      </KunButton>
    </div>
    <DeleteQueue mode="review" />
  </div>
</template>
