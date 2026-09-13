<script setup lang="ts">
import { AUTH_ART } from '~/constants/auth-art'

const auth = useAuth()

const email = ref('')
const error = ref('')
const success = ref(false)
const isLoading = ref(false)

const goLogin = () => navigateTo('/auth/login')

const handleSubmit = async () => {
  error.value = ''
  isLoading.value = true

  try {
    const response = await auth.forgotPassword(email.value)
    if (response.code === 0) {
      success.value = true
    } else {
      error.value = response.message || '发送失败'
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '发送失败'
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <AuthShell :art="AUTH_ART.forgot">
    <template v-if="success">
      <AuthOutcome
        icon="lucide:mail-check"
        title="请检查你的邮箱"
        description="如果该邮箱已注册，我们已把密码重置链接寄了过去。链接有效期有限，请尽快使用。"
      />
      <KunButton color="primary" size="lg" full-width @click="goLogin">
        返回登录
      </KunButton>
    </template>

    <template v-else>
      <AuthHeading title="重置密码" subtitle="输入账号绑定的邮箱，我们会寄出重置链接" />

      <form @submit.prevent="handleSubmit">
        <div class="space-y-5">
          <KunInput
            v-model="email"
            label="邮箱"
            type="email"
            size="lg"
            placeholder="请输入邮箱"
            required
            autofocus
          />

          <AuthNotice v-if="error">{{ error }}</AuthNotice>

          <KunButton
            type="submit"
            color="primary"
            size="lg"
            full-width
            :loading="isLoading"
            :disabled="isLoading"
          >
            {{ isLoading ? '发送中...' : '发送重置链接' }}
          </KunButton>
        </div>
      </form>

      <p class="text-default-500 mt-8 text-center text-sm">
        想起来了？
        <NuxtLink to="/auth/login" class="text-primary font-medium hover:underline">
          返回登录
        </NuxtLink>
      </p>
    </template>
  </AuthShell>
</template>
