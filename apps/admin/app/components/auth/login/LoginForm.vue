<script setup lang="ts">
const auth = useAuth()
const router = useRouter()
const route = useRoute()

const FEDERATION_ERROR_MESSAGES: Record<string, string> = {
  federation_denied: '已取消第三方授权',
  federation_state: '第三方登录已过期，请重新尝试',
  federation_failed: '第三方登录失败，请稍后重试或使用密码登录',
  federation_banned: '账号已被封禁',
  federation_stepup: '管理员账号请使用密码登录',
  federation_conflict:
    '该邮箱对应的账号已绑定其他同类第三方账号，请使用密码登录',
  federation_disabled: '该第三方登录方式未启用'
}

const account = ref((route.query.account as string) || '')
const password = ref('')
const error = ref(
  typeof route.query.error === 'string'
    ? (FEDERATION_ERROR_MESSAGES[route.query.error] ?? '')
    : ''
)
const isLoading = ref(false)
const sameAccountName = ref('')

const redirectUrl = computed(() => route.query.redirect as string | undefined)

const forceLogin = computed(() => route.query.force === '1')
const reauth = computed(() => route.query.reauth === '1')

const isSafeRedirect = (url: string): boolean => {
  if (url.startsWith('//')) return false
  if (url.startsWith('/')) return true
  try {
    return new URL(url).origin === window.location.origin
  } catch {
    return false
  }
}

const navigateAfterLogin = () => {
  const r = redirectUrl.value
  if (r && isSafeRedirect(r)) {
    if (r.startsWith('/')) router.push(r)
    else window.location.href = r
  } else {
    router.push(auth.isAdmin.value ? '/' : '/profile')
  }
}

const handleSubmit = async () => {
  error.value = ''
  sameAccountName.value = ''
  isLoading.value = true

  try {
    const prevUuid = auth.user.value?.uuid
    const response = await auth.login(account.value, password.value)
    if (response.code !== 0) {
      error.value = response.message || '登录失败'
      return
    }

    const next = response.data.user
    // Re-logged the account that's already active → no-op. Don't silently
    if (!reauth.value && prevUuid && next.uuid === prevUuid) {
      sameAccountName.value = next.name
      return
    }
    if (!redirectUrl.value && forceLogin.value && prevUuid) {
      useKunMessage(`已切换到「${next.name}」`, 'success')
    }
    navigateAfterLogin()
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '登录失败'
  } finally {
    isLoading.value = false
  }
}

const goBack = () => router.back()

onMounted(async () => {
  if (
    typeof route.query.error === 'string' &&
    FEDERATION_ERROR_MESSAGES[route.query.error]
  ) {
    const nextQuery = { ...route.query }
    delete nextQuery.error
    await router.replace({ query: nextQuery })
  }

  if (auth.isLoggedIn.value && !forceLogin.value) {
    navigateAfterLogin()
  }
})
</script>

<template>
  <AuthShell>
    <div class="mb-8">
      <h1 class="text-foreground text-2xl font-bold">
        {{ forceLogin ? '登录其他账号' : '欢迎回来' }}
      </h1>
      <p class="text-default-500 mt-2 text-sm">
        {{ forceLogin ? '登录另一个账号以添加或切换' : '登录 鲲 Galgame OAuth 管理后台' }}
      </p>
    </div>

    <div v-if="sameAccountName" class="bg-primary-50 mb-6 rounded-xl p-4 text-sm">
      <p class="text-foreground">
        这已是你当前登录的账号「<span class="font-medium">{{ sameAccountName }}</span>」
      </p>
      <p class="text-default-500 mt-1">想换一个账号？在下方重新输入即可。</p>
      <KunButton
        v-if="redirectUrl"
        color="primary"
        variant="flat"
        size="sm"
        class="mt-3"
        @click="navigateAfterLogin"
      >
        继续访问应用
      </KunButton>
      <KunButton
        v-else
        color="primary"
        variant="flat"
        size="sm"
        class="mt-3"
        @click="goBack"
      >
        返回
      </KunButton>
    </div>

    <form @submit.prevent="handleSubmit">
      <div class="space-y-5">
        <KunInput
          v-model="account"
          label="账号"
          type="text"
          placeholder="请输入邮箱或用户名"
          required
          autofocus
        />

        <KunInput
          v-model="password"
          label="密码"
          type="password"
          placeholder="请输入密码"
          required
        />

        <div v-if="error" class="bg-danger-50 text-danger rounded-xl p-3 text-sm">
          {{ error }}
        </div>

        <KunButton type="submit" color="primary" size="lg" class="w-full" :disabled="isLoading">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          {{ isLoading ? '登录中...' : '登录' }}
        </KunButton>
      </div>
    </form>

    <AuthFederationButtons />

    <div class="border-default-200 mt-8 flex flex-col gap-3 border-t pt-6 text-sm">
      <NuxtLink to="/auth/forgot-password" class="text-primary hover:underline">
        忘记密码？
      </NuxtLink>
      <p class="text-default-500">
        还没有账号？
        <NuxtLink
          :to="redirectUrl ? `/auth/register?redirect=${encodeURIComponent(redirectUrl)}` : '/auth/register'"
          class="text-primary hover:underline"
        >立即注册</NuxtLink>
      </p>
    </div>
  </AuthShell>
</template>
