<script setup lang="ts">
import { AUTH_ART } from '~/constants/auth-art'

const auth = useAuth()
const router = useRouter()
const route = useRoute()

const FEDERATION_ERROR_MESSAGES: Record<string, string> = {
  federation_denied: '已取消第三方授权',
  federation_state: '第三方登录已过期，请重新尝试',
  federation_failed: '第三方登录失败，请稍后重试或使用密码登录',
  federation_banned: '账号已被封禁',
  // The gate that emits this is hasAdminOrRen in federation_service.go — it
  // covers ren too, so the copy cannot name a single role.
  federation_stepup: '该账号权限较高，请使用密码登录',
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
    router.push('/profile')
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
  <AuthShell :art="AUTH_ART.login">
    <AuthHeading
      :title="forceLogin ? '登录其他账号' : '欢迎回来'"
      :subtitle="
        forceLogin ? '登录另一个账号以添加或切换' : '使用 NextMoe·未萌 账号继续'
      "
    />

    <div
      v-if="sameAccountName"
      class="border-default-200 bg-default-50 mb-6 rounded-xl border p-4 text-sm"
    >
      <p class="text-foreground">
        这已是你当前登录的账号「<span class="font-medium">{{ sameAccountName }}</span>」
      </p>
      <p class="text-default-500 mt-1">想换一个账号？在下方重新输入即可。</p>
      <KunButton
        color="primary"
        variant="flat"
        size="sm"
        class="mt-3"
        @click="redirectUrl ? navigateAfterLogin() : goBack()"
      >
        {{ redirectUrl ? '继续访问应用' : '返回' }}
      </KunButton>
    </div>

    <form @submit.prevent="handleSubmit">
      <div class="space-y-5">
        <KunInput
          v-model="account"
          label="账号"
          type="text"
          size="lg"
          placeholder="邮箱或用户名"
          required
          autofocus
        />

        <div>
          <KunInput
            v-model="password"
            label="密码"
            type="password"
            size="lg"
            placeholder="请输入密码"
            required
            reveal-password
          />
          <div class="mt-2 flex justify-end">
            <NuxtLink
              to="/auth/forgot-password"
              class="text-default-400 hover:text-primary text-xs transition-colors"
            >
              忘记密码？
            </NuxtLink>
          </div>
        </div>

        <AuthNotice v-if="error">{{ error }}</AuthNotice>

        <KunButton
          type="submit"
          color="primary"
          size="lg"
          full-width
          :loading="isLoading"
          :disabled="isLoading"
        >
          {{ isLoading ? '登录中...' : '登录' }}
        </KunButton>
      </div>
    </form>

    <AuthFederationButtons />

    <p class="text-default-500 mt-8 text-center text-sm">
      还没有账号？
      <NuxtLink
        :to="
          redirectUrl
            ? `/auth/register?redirect=${encodeURIComponent(redirectUrl)}`
            : '/auth/register'
        "
        class="text-primary font-medium hover:underline"
      >立即注册</NuxtLink>
    </p>
  </AuthShell>
</template>
