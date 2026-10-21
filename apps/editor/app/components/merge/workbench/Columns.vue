<script setup lang="ts">
const { preview } = useMergeWorkbench()
</script>

<template>
  <div
    v-if="preview"
    class="border-default-200 bg-background sticky top-0 z-10 -mx-4 hidden border-b px-4 py-2 md:-mx-8 md:grid md:grid-cols-3 md:gap-3 md:px-8"
  >
    <div class="flex min-w-0 items-center gap-2 text-sm">
      <span class="bg-warning-500 size-2 shrink-0 rounded-full" />
      <span class="text-default-500 shrink-0">被并入</span>
      <span class="truncate font-medium">{{ preview.source.name }}</span>
    </div>
    <div class="flex min-w-0 items-center gap-2 text-sm">
      <span class="bg-success-500 size-2 shrink-0 rounded-full" />
      <span class="text-default-500 shrink-0">保留</span>
      <span class="truncate font-medium">{{ preview.target.name }}</span>
    </div>
    <div class="flex min-w-0 items-center gap-2 text-sm">
      <span class="bg-primary-500 size-2 shrink-0 rounded-full" />
      <span class="text-default-500 shrink-0">合并后</span>
      <span class="truncate font-medium">{{ preview.target.name }}</span>
    </div>
  </div>
</template>
