<script setup lang="ts">
const route = useRoute()
const auth = useAuth()
const { startLogin } = useOAuthLogin()

const error = ref('')

onMounted(async () => {
  const code = route.query.code as string | undefined
  const state = route.query.state as string | undefined
  const oauthError = route.query.error as string | undefined

  if (oauthError) {
    error.value = '授权被取消或失败，请重试'
    return
  }
  if (!code || !state) {
    error.value = '登录校验失败，请重新登录'
    return
  }

  let dest: string
  try {
    const res = await $fetch<{
      code: number
      message?: string
      data?: { access_token: string; redirect: string | null }
    }>('/auth/exchange', {
      method: 'POST',
      body: { code, state }
    })
    if (res.code !== 0 || !res.data?.access_token) {
      error.value = res.message || '登录失败，请重试'
      return
    }
    auth.setAccessToken(res.data.access_token)
    await auth.fetchUser()
    dest = res.data.redirect ?? '/'
  } catch (e) {
    error.value =
      (e as { data?: { message?: string } }).data?.message ||
      '登录失败，请重试'
    return
  }

  await navigateTo(dest)
})

const retry = async () => {
  await startLogin('/')
}
</script>

<template>
  <div
    class="flex min-h-screen flex-col items-center justify-center gap-4 bg-background px-4 text-center"
  >
    <template v-if="error">
      <KunIcon name="lucide:circle-alert" class="size-10 text-danger" />
      <p class="text-foreground">{{ error }}</p>
      <KunButton color="primary" @click="retry">返回重新登录</KunButton>
    </template>
    <template v-else>
      <KunIcon
        name="lucide:loader-circle"
        class="size-8 animate-spin text-primary"
      />
      <p class="text-default-500">正在登录…</p>
    </template>
  </div>
</template>
