<script setup lang="ts">
const auth = useAuth()
const userStore = useUserStore()

const newEmail = ref('')
const code = ref('')
const error = ref('')
const success = ref('')
const isLoading = ref(false)
const codeSent = ref(false)
const countdown = ref(0)

let timer: ReturnType<typeof setInterval> | null = null

const startCountdown = () => {
  countdown.value = 60
  timer = setInterval(() => {
    countdown.value--
    if (countdown.value <= 0 && timer) {
      clearInterval(timer)
      timer = null
    }
  }, 1000)
}

const handleSendCode = async () => {
  error.value = ''
  success.value = ''

  if (!newEmail.value) {
    error.value = '请输入新邮箱'
    return
  }

  isLoading.value = true
  try {
    const response = await auth.sendEmailChangeCode(newEmail.value)
    if (response.code === 0) {
      codeSent.value = true
      success.value = '验证码已发送到当前邮箱，请查收'
      startCountdown()
    } else {
      error.value = response.message || '发送失败'
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '发送失败'
  } finally {
    isLoading.value = false
  }
}

const handleChangeEmail = async () => {
  error.value = ''
  success.value = ''

  if (!code.value || code.value.length !== 6) {
    error.value = '请输入 6 位验证码'
    return
  }

  isLoading.value = true
  try {
    const response = await auth.changeEmail(code.value, newEmail.value)
    if (response.code === 0) {
      success.value = '邮箱修改成功'
      if (userStore.user) {
        userStore.setUser({ ...userStore.user, email: newEmail.value })
      }
      codeSent.value = false
      newEmail.value = ''
      code.value = ''
    } else {
      error.value = response.message || '修改失败'
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '修改失败'
  } finally {
    isLoading.value = false
  }
}

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <KunCard class="p-6">
    <h3 class="mb-4 text-lg font-semibold text-foreground">
      <KunIcon name="lucide:mail" class="mr-2 inline size-5" />
      修改邮箱
    </h3>

    <p class="text-default-400 mb-4 text-sm">
      当前邮箱：{{ auth.user.value?.email }}
    </p>

    <div class="space-y-4">
      <KunInput
        v-model="newEmail"
        label="新邮箱"
        type="email"
        placeholder="请输入新邮箱地址"
        :disabled="codeSent && countdown > 0"
        autocomplete="email"
      />

      <div v-if="codeSent" class="space-y-4">
        <KunInput
          v-model="code"
          label="验证码"
          type="text"
          placeholder="请输入 6 位验证码"
          maxlength="6"
          autocomplete="one-time-code"
        />
      </div>

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div v-if="success" class="rounded-lg bg-success-50 p-3 text-sm text-success">
        {{ success }}
      </div>

      <div class="flex gap-3">
        <KunButton
          v-if="!codeSent"
          color="primary"
          class="w-full"
          :disabled="isLoading || !newEmail"
          @click="handleSendCode"
        >
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          发送验证码
        </KunButton>

        <template v-else>
          <KunButton
            color="default"
            :disabled="countdown > 0 || isLoading"
            @click="handleSendCode"
          >
            {{ countdown > 0 ? `${countdown}s` : '重新发送' }}
          </KunButton>
          <KunButton
            color="primary"
            class="flex-1"
            :disabled="isLoading || !code"
            @click="handleChangeEmail"
          >
            <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
            确认修改
          </KunButton>
        </template>
      </div>
    </div>
  </KunCard>
</template>
