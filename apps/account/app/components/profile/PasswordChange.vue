<script setup lang="ts">
const auth = useAuth()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const error = ref('')
const success = ref('')
const isLoading = ref(false)

const handleSubmit = async () => {
  error.value = ''
  success.value = ''

  if (newPassword.value.length < 6) {
    error.value = '新密码至少 6 个字符'
    return
  }

  if (newPassword.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }

  isLoading.value = true
  try {
    const response = await auth.changePassword(oldPassword.value, newPassword.value)
    if (response.code === 0) {
      success.value = '密码修改成功'
      oldPassword.value = ''
      newPassword.value = ''
      confirmPassword.value = ''
    } else {
      error.value = response.message || '修改失败'
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '修改失败'
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunCard class="p-6">
    <h3 class="mb-4 text-lg font-semibold text-foreground">
      <KunIcon name="lucide:lock" class="mr-2 inline size-5" />
      修改密码
    </h3>

    <form class="space-y-4" @submit.prevent="handleSubmit">
      <KunInput
        v-model="oldPassword"
        label="当前密码"
        type="password"
        placeholder="请输入当前密码"
        autocomplete="current-password"
      />

      <KunInput
        v-model="newPassword"
        label="新密码"
        type="password"
        placeholder="请输入新密码（至少 6 位）"
        autocomplete="new-password"
      />

      <KunInput
        v-model="confirmPassword"
        label="确认新密码"
        type="password"
        placeholder="请再次输入新密码"
        autocomplete="new-password"
      />

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div v-if="success" class="rounded-lg bg-success-50 p-3 text-sm text-success">
        {{ success }}
      </div>

      <KunButton type="submit" color="primary" class="w-full" :disabled="isLoading">
        <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
        {{ isLoading ? '修改中...' : '修改密码' }}
      </KunButton>

      <p class="text-center text-sm">
        <NuxtLink to="/auth/forgot-password" class="text-primary hover:underline">
          忘记当前密码？
        </NuxtLink>
      </p>
    </form>
  </KunCard>
</template>
