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
  <div class="flex min-h-screen items-center justify-center">
    <KunIcon
      name="lucide:loader-circle"
      class="text-primary size-8 animate-spin"
    />
  </div>
</template>
