<script setup lang="ts">
const { preview, previewError } = useSplitWorkbench()

const groups = computed(() => {
  const p = preview.value
  if (!p) {
    return []
  }
  return [
    {
      key: 'blockers',
      items: p.blockers,
      icon: 'lucide:octagon-x',
      title: '现在不能提交',
      tone: 'border-danger-200 bg-danger-50 text-danger-800'
    },
    {
      key: 'warnings',
      items: p.warnings,
      icon: 'lucide:triangle-alert',
      title: '拆分前请先看这些',
      tone: 'border-warning-200 bg-warning-50 text-warning-800'
    },
    {
      key: 'facts',
      items: p.facts,
      icon: 'lucide:search-check',
      title: '各资料站怎么收录',
      tone: 'border-default-200 bg-content1 text-foreground'
    }
  ].filter((group) => group.items.length)
})
</script>

<template>
  <div class="space-y-3">
    <KunInfo
      v-if="previewError"
      color="danger"
      variant="flat"
      icon="lucide:circle-x"
      title="无法计算拆分预览"
      :description="previewError"
    />
    <div
      v-for="group in groups"
      :key="group.key"
      :class="['rounded-xl border px-4 py-3', group.tone]"
    >
      <p class="flex items-center gap-2 text-sm font-medium">
        <KunIcon :name="group.icon" class="size-4 shrink-0" />
        {{ group.title }}
      </p>
      <ul class="mt-1.5 list-disc space-y-1 pl-9 text-sm leading-6">
        <li v-for="item in group.items" :key="item.code + item.message">
          {{ item.message }}
        </li>
      </ul>
    </div>
  </div>
</template>
