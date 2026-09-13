<script setup lang="ts">
const auth = useAuth()

if (auth.isLoggedIn.value) {
  await navigateTo('/profile', { replace: true })
}

onMounted(async () => {
  if (auth.isLoggedIn.value) return
  if (!auth.user.value) {
    await auth.fetchUser()
  }
  await navigateTo(auth.isLoggedIn.value ? '/profile' : '/auth/login', {
    replace: true
  })
})
</script>

<template>
  <div
    class="bg-background flex min-h-svh flex-col items-center justify-center gap-6"
  >
    <img
      src="/favicon.webp"
      alt=""
      width="56"
      height="56"
      class="border-default-200 size-14 rounded-2xl border object-cover"
    >
    <KunIcon
      name="lucide:loader-circle"
      class="text-primary size-6 animate-spin"
    />
  </div>
</template>
