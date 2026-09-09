<script setup lang="ts">
const colorMode = useColorMode()


const colorModeOptions = [
  { value: 'light', label: '浅色', icon: 'lucide:sun' },
  { value: 'dark', label: '深色', icon: 'lucide:moon' },
  { value: 'system', label: '跟随系统', icon: 'lucide:monitor' },
] as const

const setColorMode = (mode: string) => {
  colorMode.preference = mode
}
</script>

<template>
  <div
    class="relative flex min-h-screen items-center justify-center bg-default-50 p-4"
  >
    <div class="absolute top-4 right-4">
      <KunPopover position="bottom-end">
        <template #trigger>
          <KunButton
            variant="light"
            size="sm"
            is-icon-only
            aria-label="切换主题"
          >
            <KunIcon name="lucide:sun-moon" class="size-5" />
          </KunButton>
        </template>

        <div class="w-36 py-1">
          <button
            v-for="option in colorModeOptions"
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
    </div>

    <div class="flex w-full justify-center">
      <slot />
    </div>
  </div>
</template>
