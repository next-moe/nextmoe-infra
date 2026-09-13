<script setup lang="ts">
const auth = useAuth()
const router = useRouter()
const colorMode = useColorMode()

const colorModeOptions = [
  { value: 'light', label: '浅色', icon: 'lucide:sun' },
  { value: 'dark', label: '深色', icon: 'lucide:moon' },
  { value: 'system', label: '跟随系统', icon: 'lucide:monitor' }
] as const

const setColorMode = (mode: string) => {
  colorMode.preference = mode
}

onMounted(() => {
  if (!auth.user.value) {
    router.push('/auth/login')
  }
})

await callOnce('auth:user', async () => {
  if (!auth.user.value) {
    await auth.fetchUser()
  }
})
</script>

<template>
  <div class="bg-background flex min-h-screen flex-col">
    <header
      class="border-default-200 bg-content1 sticky top-0 z-30 flex h-16 items-center justify-between gap-2 border-b px-4 shadow-sm md:px-6"
    >
      <NuxtLink to="/profile" class="flex min-w-0 items-center gap-2">
        <img
          src="/favicon.webp"
          alt="NextMoe·未萌 账号"
          class="size-8 shrink-0 rounded-lg"
        />
        <span class="text-primary truncate text-lg font-bold">
          NextMoe·未萌 账号
        </span>
      </NuxtLink>

      <div class="flex shrink-0 items-center gap-2 md:gap-4">
        <KunPopover position="bottom-end">
          <template #trigger>
            <KunButton
              variant="light"
              size="md"
              is-icon-only
              aria-label="切换主题"
            >
              <KunIcon name="lucide:sun-moon" class="size-6" />
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

        <LayoutAccountSwitcher />
      </div>
    </header>

    <main class="flex-1 p-4 md:p-6">
      <slot />
    </main>
  </div>
</template>
