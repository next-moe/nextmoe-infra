<script setup lang="ts">
const auth = useAuth()

const email = ref('')
const error = ref('')
const success = ref(false)
const isLoading = ref(false)

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
  <AuthShell>
    <div class="mb-8">
      <h1 class="text-foreground text-2xl font-bold">重置密码</h1>
      <p class="text-default-500 mt-2 text-sm">输入您的邮箱以接收重置链接</p>
    </div>

    <div v-if="success">
      <div class="bg-success-50 mb-4 inline-flex size-14 items-center justify-center rounded-2xl">
        <KunIcon name="lucide:check" class="text-success size-7" />
      </div>
      <h2 class="text-foreground mb-2 text-lg font-semibold">请检查您的邮箱</h2>
      <p class="text-default-500 mb-6 text-sm">如果该邮箱已注册，我们已发送密码重置链接。</p>
      <NuxtLink to="/auth/login" class="text-primary text-sm hover:underline">返回登录</NuxtLink>
    </div>

    <form v-else @submit.prevent="handleSubmit">
      <div class="space-y-5">
        <KunInput v-model="email" label="邮箱" type="email" placeholder="请输入邮箱" required autofocus />

        <div v-if="error" class="bg-danger-50 text-danger rounded-xl p-3 text-sm">{{ error }}</div>

        <KunButton type="submit" color="primary" size="lg" class="w-full" :disabled="isLoading">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          {{ isLoading ? '发送中...' : '发送重置链接' }}
        </KunButton>
      </div>
    </form>

    <div v-if="!success" class="border-default-200 mt-8 border-t pt-6 text-sm">
      <NuxtLink to="/auth/login" class="text-primary hover:underline">返回登录</NuxtLink>
    </div>
  </AuthShell>
</template>
