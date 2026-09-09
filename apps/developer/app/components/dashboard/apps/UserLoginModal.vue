<script setup lang="ts">
import {
  DEV_USER_LOGIN_SCOPES,
  MAX_REDIRECT_URIS_PER_APP
} from '~/constants/dev'
import type { DevApp } from '~~/shared/types/dev'

const props = defineProps<{ app: DevApp }>()
const emit = defineEmits<{ updated: [DevApp] }>()

const open = defineModel<boolean>('open', { required: true })
const api = useApi()

const redirectUris = ref<string[]>([''])
const scopes = ref<string[]>([])
const error = ref('')
const isLoading = ref(false)

const isConfigured = computed(() => Boolean(props.app.user_login))

const reset = () => {
  const configured = props.app.user_login?.redirect_uris ?? []
  redirectUris.value = configured.length ? [...configured] : ['']
  const granted = props.app.user_login?.scopes ?? []
  scopes.value = DEV_USER_LOGIN_SCOPES.filter((s) =>
    granted.includes(s.value)
  ).map((s) => s.value)
  error.value = ''
}

watch(open, (val) => {
  if (val) reset()
})

const addUri = () => {
  if (redirectUris.value.length < MAX_REDIRECT_URIS_PER_APP) {
    redirectUris.value.push('')
  }
}

const removeUri = (index: number) => {
  if (redirectUris.value.length > 1) redirectUris.value.splice(index, 1)
}

const toggleScope = (s: string) => {
  const i = scopes.value.indexOf(s)
  if (i >= 0) scopes.value.splice(i, 1)
  else scopes.value.push(s)
}

const ipv4Octets = (host: string): number[] | null => {
  const parts = host.split('.')
  if (parts.length !== 4) return null
  const octets = parts.map((p) => (/^\d{1,3}$/.test(p) ? Number(p) : -1))
  return octets.every((n) => n >= 0 && n <= 255) ? octets : null
}

const isIpHost = (host: string) =>
  host.startsWith('[') || ipv4Octets(host) !== null

const isLoopbackHost = (host: string) => {
  if (host === '[::1]') return true
  const octets = ipv4Octets(host)
  return octets !== null && octets[0] === 127
}

const uriError = (raw: string): string => {
  if (raw.includes('*')) return `回调地址「${raw}」不能包含通配符 *`
  if (raw.includes('#')) return `回调地址「${raw}」不能带 fragment（#）`
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    return `回调地址「${raw}」不是一个合法的绝对 URL`
  }
  if (url.username || url.password) {
    return `回调地址「${raw}」不能带 userinfo（user:pass@）`
  }
  if (!url.hostname) return `回调地址「${raw}」缺少主机名`
  if (url.protocol === 'https:') {
    return isIpHost(url.hostname)
      ? `回调地址「${raw}」的 https 主机不能是裸 IP`
      : ''
  }
  if (url.protocol === 'http:') {
    return isLoopbackHost(url.hostname)
      ? ''
      : `回调地址「${raw}」：明文 http 只收 127.0.0.1 / [::1] 环回，localhost 按名拒`
  }
  return `回调地址「${raw}」只收 https:// 或环回 http://，不支持自定义 scheme`
}

const handleSubmit = async () => {
  error.value = ''
  const uris = redirectUris.value.map((u) => u.trim()).filter(Boolean)
  if (uris.length === 0) {
    error.value = '请至少填写一个回调地址'
    return
  }
  if (uris.length > MAX_REDIRECT_URIS_PER_APP) {
    error.value = `回调地址最多 ${MAX_REDIRECT_URIS_PER_APP} 个`
    return
  }
  const invalid = uris.map(uriError).find(Boolean)
  if (invalid) {
    error.value = invalid
    return
  }

  isLoading.value = true
  try {
    const res = await api.patch<DevApp>(`/dev/apps/${props.app.client_id}`, {
      user_login: { redirect_uris: uris, scopes: scopes.value }
    })
    if (res.code === 0 && res.data) {
      useKunMessage(isConfigured.value ? '已保存' : '用户登录已开启', 'success')
      open.value = false
      emit('updated', res.data)
    } else {
      error.value = res.message || '保存失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" size="lg" aria-label="配置用户登录">
    <div class="space-y-4">
      <h2 class="text-foreground text-xl font-bold">
        {{ isConfigured ? '编辑用户登录' : '开启用户登录' }}
      </h2>

      <div>
        <span class="text-default-500 mb-1 block text-sm font-medium">
          回调地址
          <span class="text-default-400 text-xs">
            — 最多 {{ MAX_REDIRECT_URIS_PER_APP }} 个
          </span>
        </span>
        <div class="space-y-2">
          <div
            v-for="(_, index) in redirectUris"
            :key="index"
            class="flex gap-2"
          >
            <KunInput
              v-model="redirectUris[index]"
              placeholder="https://example.com/auth/callback"
              class="flex-1"
            />
            <KunButton
              v-if="redirectUris.length > 1"
              variant="light"
              color="danger"
              size="sm"
              is-icon-only
              aria-label="移除回调地址"
              class-name="shrink-0"
              @click="removeUri(index)"
            >
              <KunIcon name="lucide:x" class="size-4" />
            </KunButton>
          </div>
        </div>
        <KunButton
          v-if="redirectUris.length < MAX_REDIRECT_URIS_PER_APP"
          variant="light"
          color="primary"
          size="sm"
          class-name="mt-2"
          @click="addUri"
        >
          <KunIcon name="lucide:plus" class="mr-1 size-3" />
          添加回调地址
        </KunButton>
        <p class="text-default-400 mt-2 text-xs">
          环回回调（<code class="font-mono">http://127.0.0.1/callback</code>）按
          RFC 8252 §7.3 端口无关匹配，注册时不必写端口；<code class="font-mono"
            >localhost</code
          >
          与自定义 scheme 不收。
        </p>
      </div>

      <div>
        <span class="text-default-500 mb-1 block text-sm font-medium">
          同意 scope（用户在同意页勾选）
        </span>
        <div class="grid gap-2 sm:grid-cols-2">
          <KunCheckBox
            v-for="s in DEV_USER_LOGIN_SCOPES"
            :key="s.value"
            :model-value="scopes.includes(s.value)"
            :label="`${s.label}（${s.value}）`"
            :description="s.description"
            color="primary"
            class-name="border-default-200 bg-content1 hover:border-primary rounded-lg border px-3 py-1.5"
            @update:model-value="toggleScope(s.value)"
          />
        </div>
        <p class="text-default-400 mt-2 text-xs">
          <code class="font-mono">openid</code> 始终授予，不必勾；<code
            class="font-mono"
            >catalog:read</code
          >
          无需注册即可在授权时请求。
        </p>
      </div>

      <p class="bg-default-100 text-default-500 rounded-lg p-3 text-sm">
        开启后强制 PKCE（S256），授权请求必须携带
        <code class="font-mono">code_challenge</code>；应用一律按 public client
        处理，不需要 <code class="font-mono">client_secret</code>。
      </p>

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :disabled="isLoading" @click="handleSubmit">
          <KunIcon
            v-if="isLoading"
            name="lucide:loader-circle"
            class="mr-2 size-4 animate-spin"
          />
          {{ isConfigured ? '保存' : '开启' }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
