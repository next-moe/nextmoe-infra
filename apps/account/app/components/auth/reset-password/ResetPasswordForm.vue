<script setup lang="ts">
import { AUTH_ART } from '~/constants/auth-art'

const auth = useAuth()
const route = useRoute()
const router = useRouter()

const token = computed(() => route.query.token as string)
const password = ref('')
const confirmPassword = ref('')
const error = ref('')
const success = ref(false)
const isLoading = ref(false)

const goLogin = () => router.push('/auth/login')

const handleSubmit = async () => {
  error.value = ''

  if (!token.value) {
    error.value = '无效的重置链接'
    return
  }
  if (password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  if (password.value.length < 6) {
    error.value = '密码长度至少为 6 位'
    return
  }

  isLoading.value = true
  try {
    const response = await auth.resetPassword(token.value, password.value)
    if (response.code === 0) {
      success.value = true
      setTimeout(() => router.push('/auth/login'), 3000)
    } else {
      error.value = response.message || '重置失败'
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '重置失败'
  } finally {
    isLoading.value = false
  }
}

onMounted(() => {
  if (!token.value) {
    error.value = '无效的重置链接，请重新申请。'
  }
})
</script>

<template>
  <AuthShell :art="AUTH_ART.reset">
    <template v-if="success">
      <AuthOutcome
        icon="lucide:shield-check"
        title="密码已更新"
        description="正在带你回到登录页面。其他设备上的登录状态需要用新密码重新建立。"
      />
      <KunButton color="primary" size="lg" full-width @click="goLogin">
        立即登录
      </KunButton>
    </template>

    <template v-else>
      <AuthHeading title="设置新密码" subtitle="设置完成后，其他设备需要重新登录" />

      <form @submit.prevent="handleSubmit">
        <div class="space-y-5">
          <KunInput
            v-model="password"
            label="新密码"
            type="password"
            size="lg"
            placeholder="至少 6 位"
            required
            autofocus
            reveal-password
          />
          <KunInput
            v-model="confirmPassword"
            label="确认密码"
            type="password"
            size="lg"
            placeholder="请再次输入新密码"
            required
            reveal-password
          />

          <AuthNotice v-if="error">{{ error }}</AuthNotice>

          <KunButton
            type="submit"
            color="primary"
            size="lg"
            full-width
            :loading="isLoading"
            :disabled="isLoading || !token"
          >
            {{ isLoading ? '重置中...' : '重置密码' }}
          </KunButton>
        </div>
      </form>

      <p class="text-default-500 mt-8 text-center text-sm">
        链接失效了？
        <NuxtLink
          to="/auth/forgot-password"
          class="text-primary font-medium hover:underline"
        >
          重新申请
        </NuxtLink>
      </p>
    </template>
  </AuthShell>
</template>
