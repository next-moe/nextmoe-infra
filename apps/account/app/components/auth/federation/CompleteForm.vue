<script setup lang="ts">
import { AUTH_ART } from '~/constants/auth-art'
import { federationLabel } from '~/constants/federation'

const auth = useAuth()
const api = useApi()
const router = useRouter()
const route = useRoute()

const token = ref('')
const pending = ref<FederationPendingResponse | null>(null)
const phase = ref<'loading' | 'expired' | 'ready'>(
  typeof route.query.token === 'string' && route.query.token
    ? 'loading'
    : 'expired'
)

const name = ref('')
const email = ref('')
const password = ref('')
const confirmPassword = ref('')
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

onUnmounted(() => {
  if (timer) clearInterval(timer)
})

const redirectUrl = computed(() => route.query.redirect as string | undefined)

const isSafeRedirect = (url: string): boolean => {
  if (url.startsWith('//')) return false
  if (url.startsWith('/')) return true
  try {
    return new URL(url).origin === window.location.origin
  } catch {
    return false
  }
}

const providerLabel = computed(() =>
  federationLabel(pending.value?.provider ?? '')
)

const emailLocked = computed(() => pending.value?.email_locked === true)

const navigateAfterComplete = () => {
  const r = redirectUrl.value
  if (r && isSafeRedirect(r)) {
    if (r.startsWith('/')) router.push(r)
    else window.location.href = r
  } else {
    router.push('/profile')
  }
}

const goLogin = () => {
  const r = redirectUrl.value
  if (r && isSafeRedirect(r)) {
    router.push(`/auth/login?redirect=${encodeURIComponent(r)}`)
  } else {
    router.push('/auth/login')
  }
}

const validateNamePassword = (): boolean => {
  if (!isValidName(name.value)) {
    error.value =
      '用户名需为 1 到 17 个字符，仅允许字母 / 数字 / 中文 / `!~_@#$%^&*()+=-`，且不可含空格、零宽字符等不可见符号'
    return false
  }
  if (password.value.length < 6) {
    error.value = '密码长度至少为 6 位'
    return false
  }
  if (password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return false
  }
  return true
}

const handleSendCode = async () => {
  error.value = ''
  success.value = ''

  if (!validateNamePassword()) return
  if (!email.value || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value)) {
    error.value = '请输入合法的邮箱地址'
    return
  }

  isLoading.value = true
  try {
    const response = await auth.sendRegisterCode(name.value, email.value)
    if (response.code === 0) {
      codeSent.value = true
      success.value = '验证码已发送到你的邮箱，请查收（如未收到请检查垃圾箱）'
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

const handleComplete = async () => {
  error.value = ''
  success.value = ''

  if (!validateNamePassword()) return
  if (!emailLocked.value) {
    if (!email.value || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value)) {
      error.value = '请输入合法的邮箱地址'
      return
    }
    if (!code.value || code.value.length !== 6) {
      error.value = '请输入 6 位验证码'
      return
    }
  }

  isLoading.value = true
  try {
    const response = await auth.completeFederation(
      emailLocked.value
        ? {
            token: token.value,
            name: name.value,
            password: password.value
          }
        : {
            token: token.value,
            name: name.value,
            password: password.value,
            email: email.value,
            code: code.value
          }
    )
    if (response.code === 0) {
      navigateAfterComplete()
    } else {
      error.value = response.message || '注册失败'
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '注册失败'
  } finally {
    isLoading.value = false
  }
}

onMounted(async () => {
  if (phase.value === 'expired') return
  const t = route.query.token
  if (typeof t !== 'string' || !t) {
    phase.value = 'expired'
    return
  }
  token.value = t
  const response = await api.get<FederationPendingResponse>(
    '/auth/federation/pending',
    { token: t }
  )
  if (response.code !== 0 || !response.data) {
    phase.value = 'expired'
    return
  }
  pending.value = response.data
  name.value = response.data.suggested_name || ''
  phase.value = 'ready'
})
</script>

<template>
  <AuthShell :art="AUTH_ART.federation">
    <div
      v-if="phase === 'loading'"
      class="text-default-500 flex items-center gap-2 py-10 text-sm"
    >
      <KunIcon name="lucide:loader-circle" class="size-4 animate-spin" />
      加载中...
    </div>

    <template v-else-if="phase === 'expired'">
      <AuthOutcome
        tone="danger"
        icon="lucide:clock-alert"
        title="链接已过期"
        description="第三方登录的这一步有时效，请回到登录页重新发起一次。"
      />
      <KunButton color="primary" size="lg" full-width @click="goLogin">
        返回登录
      </KunButton>
    </template>

    <template v-else>
      <AuthHeading
        title="完成注册"
        :subtitle="`通过 ${providerLabel} 登录还差一步`"
      />

      <form @submit.prevent>
        <div class="space-y-4">
          <KunInput
            v-model="name"
            label="用户名"
            type="text"
            size="lg"
            placeholder="1 到 17 字符"
            required
            autofocus
            :disabled="codeSent"
          />

          <div
            v-if="emailLocked"
            class="border-default-200 bg-default-50 rounded-xl border p-4"
          >
            <p class="text-default-400 text-xs">邮箱</p>
            <p class="text-foreground mt-1 text-sm break-all">
              {{ pending?.email }}
            </p>
            <p class="text-default-500 mt-2 flex items-center gap-1.5 text-xs">
              <KunIcon name="lucide:badge-check" class="text-success size-3.5" />
              来自 {{ providerLabel }} 的已验证邮箱
            </p>
          </div>
          <KunInput
            v-else
            v-model="email"
            label="邮箱"
            type="email"
            size="lg"
            placeholder="将寄送验证码到此邮箱"
            required
            :disabled="codeSent"
          />

          <KunInput
            v-model="password"
            label="密码"
            type="password"
            size="lg"
            placeholder="至少 6 位"
            required
            reveal-password
            :disabled="codeSent"
          />
          <KunInput
            v-model="confirmPassword"
            label="确认密码"
            type="password"
            size="lg"
            placeholder="请再次输入密码"
            required
            reveal-password
            :disabled="codeSent"
          />

          <KunInput
            v-if="!emailLocked && codeSent"
            v-model="code"
            label="验证码"
            type="text"
            size="lg"
            placeholder="请输入 6 位验证码"
            maxlength="6"
            autofocus
          />

          <AuthNotice v-if="error">{{ error }}</AuthNotice>
          <AuthNotice v-if="success" tone="success">{{ success }}</AuthNotice>

          <KunButton
            v-if="emailLocked"
            type="button"
            color="primary"
            size="lg"
            full-width
            :loading="isLoading"
            :disabled="isLoading"
            @click="handleComplete"
          >
            {{ isLoading ? '注册中...' : '完成注册' }}
          </KunButton>

          <div v-else class="flex gap-3">
            <KunButton
              v-if="!codeSent"
              type="button"
              color="primary"
              size="lg"
              full-width
              :loading="isLoading"
              :disabled="isLoading"
              @click="handleSendCode"
            >
              {{ isLoading ? '发送中...' : '发送验证码' }}
            </KunButton>

            <template v-else>
              <KunButton
                type="button"
                color="default"
                variant="bordered"
                size="lg"
                :disabled="countdown > 0 || isLoading"
                @click="handleSendCode"
              >
                {{ countdown > 0 ? `${countdown}s` : '重新发送' }}
              </KunButton>
              <KunButton
                type="button"
                color="primary"
                size="lg"
                class="flex-1"
                :loading="isLoading"
                :disabled="isLoading || !code"
                @click="handleComplete"
              >
                {{ isLoading ? '注册中...' : '完成注册' }}
              </KunButton>
            </template>
          </div>
        </div>
      </form>
    </template>
  </AuthShell>
</template>
