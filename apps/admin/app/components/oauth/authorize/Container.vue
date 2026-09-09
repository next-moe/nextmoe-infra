<script setup lang="ts">
import { needsStepUp } from '~/constants/roles'
import { SCOPE_LABELS } from '~~/shared/types/oauth-client'

interface ClientPublicInfo {
  id: string
  name: string
  auto_consent: boolean
  site_domain: string
  logo_url?: string
  third_party: boolean
}

const route = useRoute()
const router = useRouter()
const auth = useAuth()
const api = useApi()
const accountSwitch = useAccountSwitch()

const isLoading = ref(false)
const error = ref('')
const showChooser = ref(false)
const bagSessions = ref<BagSession[]>([])
const switchingSub = ref<string | null>(null)
const clientInfo = ref<ClientPublicInfo | null | undefined>(undefined)
const autoConsenting = ref(false)
const needsLogin = ref(false)

const clientId = computed(() => route.query.client_id as string)
const redirectUri = computed(() => route.query.redirect_uri as string)
const responseType = computed(() => route.query.response_type as string)
const scope = computed(() => route.query.scope as string)
const state = computed(() => route.query.state as string)
const codeChallenge = computed(() => route.query.code_challenge as string | undefined)
const codeChallengeMethod = computed(() => route.query.code_challenge_method as string | undefined)
const forceLogin = computed(() => route.query.prompt === 'login')
const promptSelectAccount = computed(() => route.query.prompt === 'select_account')
const promptNone = computed(() => route.query.prompt === 'none')
const loginHint = computed(() => route.query.login_hint as string | undefined)
const nonce = computed(() => route.query.nonce as string | undefined)

const buildAuthorizeUrl = (extra?: Record<string, string>) => {
  const params = new URLSearchParams()
  params.set('client_id', clientId.value)
  params.set('redirect_uri', redirectUri.value)
  params.set('response_type', responseType.value)
  if (scope.value) params.set('scope', scope.value)
  params.set('state', state.value)
  if (codeChallenge.value) params.set('code_challenge', codeChallenge.value)
  if (codeChallengeMethod.value) params.set('code_challenge_method', codeChallengeMethod.value)
  if (nonce.value) params.set('nonce', nonce.value)
  if (extra) {
    for (const [k, v] of Object.entries(extra)) params.set(k, v)
  }
  return `/oauth/authorize?${params.toString()}`
}

const currentUrl = computed(() => buildAuthorizeUrl())

const scopeList = computed(() => {
  if (!scope.value) return []
  return scope.value.split(/[\s+]/).filter(Boolean)
})

const respondWithError = async (errCode: string): Promise<void> => {
  const res = await api.post<{ redirect_url: string }>(
    '/oauth/authorize/error',
    {
      client_id: clientId.value,
      redirect_uri: redirectUri.value,
      state: state.value,
      error: errCode,
    }
  )
  if (res.code === 0) {
    window.location.href = res.data.redirect_url
    return
  }
  autoConsenting.value = false
  needsLogin.value = false
  error.value = res.message || '回调地址未通过校验，未跳转'
}

onMounted(async () => {
  if (!clientId.value || !redirectUri.value || !state.value) {
    error.value = '缺少必要的 OAuth 参数'
    return
  }

  if (forceLogin.value) {
    needsLogin.value = true
  } else if (!auth.isLoggedIn.value) {
    const refreshed = await auth.refreshAccessToken()
    if (!refreshed) {
      needsLogin.value = true
    }
  }

  try {
    const meta = await api.get<ClientPublicInfo>('/oauth/client-info', {
      client_id: clientId.value,
    })
    if (meta.code === 0) {
      clientInfo.value = meta.data
    } else {
      clientInfo.value = null
    }
  } catch {
    clientInfo.value = null
  }

  if (promptNone.value) {
    if (needsLogin.value || !auth.isLoggedIn.value) {
      await respondWithError('login_required')
      return
    }
    if (!clientInfo.value?.auto_consent) {
      await respondWithError('consent_required')
      return
    }
    await maybeAutoConsent()
    return
  }

  if (!needsLogin.value && auth.isLoggedIn.value) {
    const handled = await handleMultiAccount()
    if (handled) return
  }

  await maybeAutoConsent()
})

const handleMultiAccount = async (): Promise<boolean> => {
  if (!loginHint.value && !promptSelectAccount.value) return false

  bagSessions.value = await accountSwitch.listBagSessions()

  if (loginHint.value) {
    const hint = loginHint.value
    const match = bagSessions.value.find(
      (s) => s.sub === hint || s.email === hint
    )
    if (match) {
      if (match.active) return false
      const result = await accountSwitch.switchAccount(match.sub)
      if (result.ok) {
        window.location.href = buildAuthorizeUrl()
        return true
      }
      if (result.stepUp) {
        redirectStepUp(match.sub)
        return true
      }
      showChooser.value = true
      return true
    }
    if (!promptSelectAccount.value) return false
  }

  showChooser.value = true
  return true
}

const maybeAutoConsent = async () => {
  if (
    !needsLogin.value &&
    auth.isLoggedIn.value &&
    clientInfo.value?.auto_consent
  ) {
    autoConsenting.value = true
    await handleApprove()
  }
}

const redirectStepUp = (sub: string) => {
  const target = buildAuthorizeUrl({ login_hint: sub })
  router.push(`/auth/login?force=1&redirect=${encodeURIComponent(target)}`)
}

const handleChooserPick = async (sub: string) => {
  if (switchingSub.value) return
  const target = bagSessions.value.find((s) => s.sub === sub)
  if (target && needsStepUp(target.roles)) {
    redirectStepUp(sub)
    return
  }
  switchingSub.value = sub
  error.value = ''
  try {
    const result = await accountSwitch.switchAccount(sub)
    if (result.ok) {
      window.location.href = buildAuthorizeUrl()
      return
    }
    if (result.stepUp) {
      redirectStepUp(sub)
      return
    }
    error.value = '切换账号失败，请重试'
  } finally {
    switchingSub.value = null
  }
}

const handleChooserAdd = () => {
  router.push(`/auth/login?force=1&redirect=${encodeURIComponent(currentUrl.value)}`)
}

const goLogin = () => {
  router.push(
    `/auth/login?force=1&reauth=1&redirect=${encodeURIComponent(currentUrl.value)}`
  )
}

const goRegister = () => {
  router.push(`/auth/register?redirect=${encodeURIComponent(currentUrl.value)}`)
}

const handleApprove = async () => {
  isLoading.value = true
  error.value = ''

  try {
    const body: Record<string, unknown> = {
      client_id: clientId.value,
      redirect_uri: redirectUri.value,
      response_type: responseType.value,
      scope: scope.value,
      state: state.value,
    }
    if (codeChallenge.value) body.code_challenge = codeChallenge.value
    if (codeChallengeMethod.value) body.code_challenge_method = codeChallengeMethod.value
    if (nonce.value) body.nonce = nonce.value

    const response = await api.post<{ redirect_url: string }>('/oauth/authorize/consent', body)

    if (response.code === 0) {
      window.location.href = response.data.redirect_url
    } else {
      error.value = response.message || '授权失败'
      autoConsenting.value = false
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '授权失败'
    autoConsenting.value = false
  } finally {
    isLoading.value = false
  }
}

const handleDeny = async () => {
  isLoading.value = true
  try {
    await respondWithError('access_denied')
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunCard class="w-full max-w-md p-8">
    <div v-if="error && !clientId" class="py-4 text-center">
      <div class="bg-danger-50 mx-auto mb-4 inline-flex size-14 items-center justify-center rounded-2xl">
        <KunIcon name="lucide:circle-alert" class="text-danger size-7" />
      </div>
      <h1 class="text-foreground text-lg font-semibold">无法完成授权</h1>
      <p class="text-danger mt-2 text-sm">{{ error }}</p>
    </div>

    <div v-else-if="needsLogin" class="space-y-6">
      <OauthAuthorizeHandshake
        :client-name="clientInfo?.name"
        :client-logo="clientInfo?.logo_url"
      />

      <div class="text-center">
        <h1 class="text-foreground text-xl font-bold">需要登录后授权</h1>
        <p class="text-default-500 mt-2 text-sm">
          <template v-if="clientInfo">
            「<span class="text-foreground font-medium">{{ clientInfo.name }}</span>」请求访问你的账户
          </template>
          <template v-else>
            一个应用请求访问你的账户
          </template>
        </p>
        <p class="text-default-400 mt-1 text-xs">
          登录后将自动完成授权，无需额外操作
        </p>
      </div>

      <div class="space-y-2">
        <KunButton color="primary" size="lg" class="w-full" @click="goLogin">
          登录后继续
        </KunButton>
        <KunButton color="default" variant="light" class="w-full" @click="handleDeny">
          取消
        </KunButton>
      </div>

      <p class="text-default-500 text-center text-sm">
        还没有账号？
        <button
          type="button"
          class="text-primary hover:underline"
          @click="goRegister"
        >立即注册</button>
      </p>
    </div>

    <div
      v-else-if="autoConsenting || clientInfo === undefined"
      class="flex min-h-40 flex-col items-center justify-center py-8 text-center"
    >
      <KunIcon name="lucide:loader-circle" class="text-primary mb-4 size-8 animate-spin" />
      <p class="text-default-500 text-sm">
        {{ autoConsenting ? '正在跳转回应用...' : '加载中...' }}
      </p>
    </div>

    <div v-else-if="showChooser" class="space-y-4">
      <OauthAuthorizeAccountChooser
        :sessions="bagSessions"
        :client-name="clientInfo?.name"
        :switching-sub="switchingSub"
        @pick="handleChooserPick"
        @add="handleChooserAdd"
      />
      <div v-if="error" class="bg-danger-50 text-danger rounded-xl p-3 text-sm">
        {{ error }}
      </div>
    </div>

    <template v-else>
      <OauthAuthorizeHandshake
        :client-name="clientInfo?.name"
        :client-logo="clientInfo?.logo_url"
      />

      <div class="mt-6 mb-6 text-center">
        <h1 class="text-foreground text-xl font-bold">授权请求</h1>
        <p class="text-default-500 mt-2 text-sm">
          <span v-if="clientInfo">「{{ clientInfo.name }}」</span>
          正在请求访问你的账户
        </p>
      </div>

      <div
        v-if="clientInfo?.third_party"
        class="bg-warning-50 border-warning-200 mb-6 flex items-start gap-2 rounded-xl border p-3"
      >
        <KunIcon
          name="lucide:triangle-alert"
          class="text-warning mt-0.5 size-4 shrink-0"
        />
        <p class="text-default-600 text-xs">
          这是<span class="text-foreground font-medium">第三方应用</span>，由站外开发者注册，不隶属于
          NextMoe。应用名称与图标由开发者自行填写，请确认你信任它再继续。
        </p>
      </div>

      <div class="bg-default-50 mb-6 space-y-3 rounded-xl p-4">
        <p class="text-foreground text-sm font-medium">该应用将获得以下权限：</p>
        <ul class="space-y-2">
          <li
            v-for="s in scopeList"
            :key="s"
            class="text-default-500 flex items-center gap-2 text-sm"
          >
            <KunIcon name="lucide:check" class="text-success size-4 shrink-0" />
            {{ SCOPE_LABELS[s] || s }}
          </li>
          <li
            v-if="scopeList.length === 0"
            class="text-default-500 flex items-center gap-2 text-sm"
          >
            <KunIcon name="lucide:check" class="text-success size-4 shrink-0" />
            基本账户信息
          </li>
        </ul>
      </div>

      <div v-if="error" class="bg-danger-50 text-danger mb-4 rounded-xl p-3 text-sm">
        {{ error }}
      </div>

      <div class="space-y-2">
        <KunButton
          color="primary"
          size="lg"
          class="w-full"
          :disabled="isLoading"
          @click="handleApprove"
        >
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          {{ isLoading ? '授权中...' : '同意授权' }}
        </KunButton>
        <KunButton color="default" variant="light" class="w-full" @click="handleDeny">
          拒绝
        </KunButton>
      </div>

      <p class="text-default-400 mt-4 text-center text-xs break-all">
        授权后将跳转回 {{ redirectUri }}
      </p>
    </template>
  </KunCard>
</template>
