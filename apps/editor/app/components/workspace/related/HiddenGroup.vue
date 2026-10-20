<script setup lang="ts">
const props = defineProps<{ count: number; label?: string }>()

const open = ref(false)
</script>

<template>
  <div v-if="props.count" class="border-default-100 border-t">
    <button
      type="button"
      class="text-default-500 hover:text-foreground hover:bg-default-50 flex w-full items-center gap-2 px-3 py-2 text-xs"
      :aria-expanded="open"
      @click="open = !open"
    >
      <KunIcon name="lucide:eye-off" class="size-3.5" />
      <span class="flex-1 text-left">
        {{ props.label ?? '已隐藏' }}
        <span class="tabular-nums">{{ props.count }}</span>
      </span>
      <KunIcon
        :name="open ? 'lucide:chevron-up' : 'lucide:chevron-down'"
        class="size-3.5"
      />
    </button>
    <div v-if="open">
      <slot />
    </div>
  </div>
</template>
