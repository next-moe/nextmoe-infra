<script setup lang="ts">
withDefaults(
  defineProps<{
    size?: 'sm' | 'md'
  }>(),
  { size: 'sm' }
)

const colorMode = useColorMode()

const options = [
  { value: 'light', label: '浅色', icon: 'lucide:sun' },
  { value: 'dark', label: '深色', icon: 'lucide:moon' },
  { value: 'system', label: '跟随系统', icon: 'lucide:monitor' }
] as const

const setColorMode = (mode: string) => {
  colorMode.preference = mode
}
</script>

<template>
  <KunPopover position="bottom-end">
    <template #trigger>
      <KunButton variant="light" :size="size" is-icon-only aria-label="切换主题">
        <KunIcon name="lucide:sun-moon" :class="size === 'sm' ? 'size-5' : 'size-6'" />
      </KunButton>
    </template>

    <div class="w-36 py-1">
      <button
        v-for="option in options"
        :key="option.value"
        class="flex w-full items-center gap-3 px-3 py-2 text-sm transition-colors"
        :class="
          colorMode.preference === option.value
            ? 'bg-primary-50 text-primary'
            : 'text-default-500 hover:bg-default-100 hover:text-foreground'
        "
        @click="setColorMode(option.value)"
      >
        <KunIcon :name="option.icon" class="size-4" />
        <span>{{ option.label }}</span>
      </button>
    </div>
  </KunPopover>
</template>
