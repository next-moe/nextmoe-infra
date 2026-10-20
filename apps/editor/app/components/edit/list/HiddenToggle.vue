<script setup lang="ts">
const props = defineProps<{ count: number }>()
const open = defineModel<boolean>({ required: true })
</script>

<template>
  <button
    v-if="props.count"
    type="button"
    class="text-default-500 hover:bg-default-100 hover:text-foreground flex w-full items-center gap-1.5 rounded-lg px-2 py-1.5 text-xs transition-colors"
    :aria-expanded="open"
    @click="open = !open"
  >
    <KunIcon name="lucide:eye-off" class="size-3.5" />
    {{ open ? '收起' : '显示' }}已隐藏的 {{ props.count }} 条
    <KunIcon
      :name="open ? 'lucide:chevron-up' : 'lucide:chevron-down'"
      class="size-3.5"
    />
  </button>
</template>
