<script setup lang="ts">
const auth = useAuth()
const route = useRoute()
const router = useRouter()

const token = computed(() => route.query.token as string)
const password = ref('')
const confirmPassword = ref('')
const error = ref('')
const success = ref(false)
const isLoading = ref(false)

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
  <AuthShell>
    <div class="mb-8">
      <h1 class="text-foreground text-2xl font-bold">设置新密码</h1>
      <p class="text-default-500 mt-2 text-sm">请输入您的新密码</p>
    </div>

    <div v-if="success">
      <div class="bg-success-50 mb-4 inline-flex size-14 items-center justify-center rounded-2xl">
        <KunIcon name="lucide:check" class="text-success size-7" />
      </div>
      <h2 class="text-foreground mb-2 text-lg font-semibold">密码重置成功</h2>
      <p class="text-default-500 mb-6 text-sm">您的密码已重置，正在跳转到登录页面...</p>
      <NuxtLink to="/auth/login" class="text-primary text-sm hover:underline">立即登录</NuxtLink>
    </div>

    <form v-else @submit.prevent="handleSubmit">
      <div class="space-y-5">
        <KunInput v-model="password" label="新密码" type="password" placeholder="请输入新密码" required autofocus />
        <KunInput v-model="confirmPassword" label="确认密码" type="password" placeholder="请再次输入新密码" required />

        <div v-if="error" class="bg-danger-50 text-danger rounded-xl p-3 text-sm">{{ error }}</div>

        <KunButton type="submit" color="primary" size="lg" class="w-full" :disabled="isLoading || !token">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          {{ isLoading ? '重置中...' : '重置密码' }}
        </KunButton>
      </div>
    </form>

    <div v-if="!success" class="border-default-200 mt-8 border-t pt-6 text-sm">
      <NuxtLink to="/auth/forgot-password" class="text-primary hover:underline">重新申请重置链接</NuxtLink>
    </div>
  </AuthShell>
</template>
