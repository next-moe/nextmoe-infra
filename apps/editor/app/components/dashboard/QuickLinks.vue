<script setup lang="ts">
const props = defineProps<{ isReviewer: boolean; canApply: boolean }>()

const links = computed(() => [
  {
    label: '浏览作品',
    description: '查找条目并提交修改',
    icon: 'lucide:gamepad-2',
    to: '/works'
  },
  ...(props.isReviewer
    ? [
        {
          label: '待审队列',
          description: '处理其他人的提案',
          icon: 'lucide:inbox',
          to: '/review'
        }
      ]
    : []),
  {
    label: '我的草稿',
    description: '继续未提交的修改',
    icon: 'lucide:file-pen-line',
    to: '/me/drafts'
  },
  {
    label: '编辑规范',
    description: '动手前先读一读',
    icon: 'lucide:book-open',
    to: '/guidelines'
  },
  ...(props.canApply
    ? [
        {
          label: '申请编辑者',
          description: '修改无需审核即可生效',
          icon: 'lucide:badge-check',
          to: '/apply'
        }
      ]
    : [])
])
</script>

<template>
  <div class="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3">
    <NuxtLink
      v-for="link in links"
      :key="link.to"
      :to="link.to"
      class="border-default-200 bg-content1 hover:border-primary-200 flex items-center gap-3 rounded-xl border p-3 transition-colors"
    >
      <span
        class="bg-default-100 text-default-600 flex size-9 shrink-0 items-center justify-center rounded-lg"
      >
        <KunIcon :name="link.icon" class="size-4" />
      </span>
      <span class="min-w-0">
        <span class="block text-sm font-medium">{{ link.label }}</span>
        <span class="text-default-500 block truncate text-xs">
          {{ link.description }}
        </span>
      </span>
    </NuxtLink>
  </div>
</template>
