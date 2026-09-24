<script setup lang="ts">
const auth = useAuth()

const code = ref('')
const error = ref('')
const isLoading = ref(false)
const codeSent = ref(false)
const countdown = ref(0)

let timer: ReturnType<typeof setInterval> | null = null

const dueAt = computed(() => auth.user.value?.deletion_due_at ?? null)

const consequences = [
  '用户名、邮箱、头像、简介会被清除，账号无法再登录',
  '绑定的 Google / GitHub 等第三方登录会被解除',
  '云端偏好、角色和所有登录设备会被清除',
  '萌萌点余额随账号作废，已购买的装扮不再可用',
  '接入的站点会按各自的隐私政策，清理你在该站保存的数据',
]

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

const failed = (e: unknown, fallback: string) => {
  error.value = e instanceof Error ? e.message : fallback
}

const handleSendCode = async () => {
  error.value = ''
  isLoading.value = true
  try {
    const response = await auth.sendDeletionCode()
    if (response.code === 0) {
      codeSent.value = true
      startCountdown()
    } else {
      error.value = response.message || '发送失败'
    }
  } catch (e: unknown) {
    failed(e, '发送失败')
  } finally {
    isLoading.value = false
  }
}

const handleRequest = async () => {
  error.value = ''
  if (code.value.length !== 6) {
    error.value = '请输入 6 位验证码'
    return
  }
  const confirmed = await useKunAlert({
    title: '确认注销账号',
    message: '7 天后账号将被永久注销且无法恢复。冷静期内你可以随时回到这里撤销。',
    showCancel: true,
    confirmText: '确认注销',
    cancelText: '再想想',
    type: 'danger',
  })
  if (!confirmed) return

  isLoading.value = true
  try {
    const response = await auth.requestDeletion(code.value)
    if (response.code === 0) {
      code.value = ''
      codeSent.value = false
      await auth.fetchUser()
    } else {
      error.value = response.message || '提交失败'
    }
  } catch (e: unknown) {
    failed(e, '提交失败')
  } finally {
    isLoading.value = false
  }
}

const handleCancel = async () => {
  error.value = ''
  isLoading.value = true
  try {
    const response = await auth.cancelDeletion()
    if (response.code === 0) {
      await auth.fetchUser()
      useKunMessage('已撤销注销', 'success')
    } else {
      error.value = response.message || '撤销失败'
    }
  } catch (e: unknown) {
    failed(e, '撤销失败')
  } finally {
    isLoading.value = false
  }
}

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="mx-auto max-w-2xl space-y-6">
    <div>
      <h1 class="text-foreground text-[1.75rem] leading-tight font-semibold tracking-tight">
        注销账号
      </h1>
      <p class="text-default-500 mt-2 text-sm">
        注销针对整个 NextMoe·未萌 账号，所有接入的站点和 App 会一起失去这个账号。
      </p>
    </div>

    <KunCard v-if="dueAt" class="border-warning-200 border p-6">
      <h3 class="text-foreground mb-2 text-lg font-semibold">
        <KunIcon name="lucide:clock" class="text-warning mr-2 inline size-5" />
        注销已排期
      </h3>
      <p class="text-default-500 mb-4 text-sm">
        账号将于 <span class="text-foreground font-medium">{{ formatDeletionDue(dueAt) }}</span>
        注销。在此之前账号照常可用，撤销后一切保持原样。
      </p>
      <div v-if="error" class="bg-danger-50 text-danger mb-4 rounded-lg p-3 text-sm">
        {{ error }}
      </div>
      <KunButton color="primary" :disabled="isLoading" @click="handleCancel">
        <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
        撤销注销
      </KunButton>
    </KunCard>

    <KunCard v-else class="border-danger-200 border p-6">
      <h3 class="text-foreground mb-3 text-lg font-semibold">
        <KunIcon name="lucide:triangle-alert" class="text-danger mr-2 inline size-5" />
        注销后
      </h3>
      <ul class="text-default-500 mb-4 list-disc space-y-1 pl-5 text-sm">
        <li v-for="item in consequences" :key="item">{{ item }}</li>
      </ul>
      <p class="text-default-500 mb-4 text-sm">
        提交后有 <span class="text-foreground font-medium">7 天冷静期</span>，到期才会执行；执行后无法恢复。
        验证码会发到当前邮箱 {{ auth.user.value?.email }}。
      </p>

      <div class="space-y-4">
        <KunInput
          v-if="codeSent"
          v-model="code"
          label="验证码"
          type="text"
          placeholder="请输入 6 位验证码"
          maxlength="6"
          autocomplete="one-time-code"
        />

        <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
          {{ error }}
        </div>

        <div class="flex gap-3">
          <KunButton
            v-if="!codeSent"
            color="danger"
            variant="light"
            class="w-full"
            :disabled="isLoading"
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
              color="danger"
              class="flex-1"
              :disabled="isLoading || code.length !== 6"
              @click="handleRequest"
            >
              <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
              申请注销
            </KunButton>
          </template>
        </div>
      </div>
    </KunCard>
  </div>
</template>
