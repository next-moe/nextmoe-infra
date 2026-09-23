<script setup lang="ts">
const auth = useAuth()
const router = useRouter()

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
  <div class="bg-background flex min-h-svh flex-col">
    <header
      class="border-default-200 bg-content1/90 sticky top-0 z-30 border-b backdrop-blur"
    >
      <div
        class="3xl:max-w-6xl mx-auto flex h-16 max-w-5xl items-center justify-between gap-2 px-4 md:px-6"
      >
        <NuxtLink to="/profile" class="flex min-w-0 items-center gap-2.5">
          <img
            src="/favicon.webp"
            alt=""
            width="32"
            height="32"
            class="border-default-200 size-8 shrink-0 rounded-lg border object-cover"
          >
          <span class="flex min-w-0 flex-col leading-tight">
            <span class="text-foreground truncate text-sm font-semibold">
              NextMoe·未萌
            </span>
            <span class="text-default-400 truncate text-xs">账号中心</span>
          </span>
        </NuxtLink>

        <div class="flex shrink-0 items-center gap-2 md:gap-3">
          <nav class="flex items-center gap-1">
            <KunButton href="/profile" size="sm" variant="light" color="default">
              资料
            </KunButton>
            <KunButton href="/shop" size="sm" variant="light" color="default">
              商店
            </KunButton>
          </nav>
          <LayoutColorModeToggle size="md" />
          <LayoutAccountSwitcher />
        </div>
      </div>
    </header>

    <main
      class="3xl:max-w-6xl mx-auto w-full max-w-5xl flex-1 p-4 md:p-6 md:py-10"
    >
      <slot />
    </main>
  </div>
</template>
