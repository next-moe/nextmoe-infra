<script setup lang="ts">
import { ALL_GRANTS, KNOWN_SCOPES, REN_ONLY_SCOPES, DEFAULT_REFRESH_TOKEN_TTL_SECONDS } from '~~/shared/types/oauth-client'

const props = defineProps<{ client: OAuthClient | null }>()
const emit = defineEmits<{ updated: [] }>()

const open = defineModel<boolean>('open', { required: true })
const api = useApi()

const { isRen } = useAuth()
const scopeOptions = computed(() =>
  isRen.value ? KNOWN_SCOPES : KNOWN_SCOPES.filter((s) => !REN_ONLY_SCOPES.includes(s))
)

const name = ref('')
const redirectUris = ref<string[]>([])
const grants = ref<string[]>([])
const allowedScopes = ref<string[]>([])
const refreshTokenTtlDays = ref(DEFAULT_REFRESH_TOKEN_TTL_SECONDS / 86400)
const isPublicReadonly = computed(() => props.client?.is_public ?? false)
const autoConsent = ref(false)
const listed = ref(false)
const logoUrl = ref('')
const tagline = ref('')
const displayOrder = ref<number | null>(0)
const error = ref('')
const isLoading = ref(false)

const initForm = (c: OAuthClient) => {
  name.value = c.name
  redirectUris.value = [...c.redirect_uris]
  grants.value = [...(c.grants ?? [])]
  allowedScopes.value = [...(c.allowed_scopes ?? [])]
  refreshTokenTtlDays.value = Math.round(
    (c.refresh_token_ttl_seconds ?? DEFAULT_REFRESH_TOKEN_TTL_SECONDS) / 86400
  )
  autoConsent.value = c.auto_consent ?? false
  listed.value = c.listed ?? false
  logoUrl.value = c.logo_url ?? ''
  tagline.value = c.tagline ?? ''
  displayOrder.value = c.display_order ?? 0
  error.value = ''
}

const { uploading: logoUploading, error: logoError, upload: uploadLogo } = useImageUpload()
const logoUploadKey = ref(0)

const onLogoPicked = async (blob: Blob) => {
  const res = await uploadLogo<{ url: string }>(
    '/oauth/clients/logo',
    blob,
    'logo.webp'
  )
  if (res) {
    logoUrl.value = res.url
    logoUploadKey.value++
  }
}

watch(open, (v) => {
  if (v && props.client) initForm(props.client)
})

const addUri = () => {
  redirectUris.value.push('')
}

const removeUri = (index: number) => {
  if (redirectUris.value.length > 1) {
    redirectUris.value.splice(index, 1)
  }
}

const toggleGrant = (g: string) => {
  const i = grants.value.indexOf(g)
  if (i >= 0) {
    grants.value.splice(i, 1)
  } else {
    grants.value.push(g)
  }
}

const toggleScope = (s: string) => {
  const i = allowedScopes.value.indexOf(s)
  if (i >= 0) {
    allowedScopes.value.splice(i, 1)
  } else {
    allowedScopes.value.push(s)
  }
}

const handleSubmit = async () => {
  const client = props.client
  if (!client) return
  error.value = ''

  if (!name.value) {
    error.value = '请填写名称'
    return
  }

  const uris = redirectUris.value.filter((u) => u.trim())
  if (uris.length === 0) {
    error.value = '请至少填写一个回调地址'
    return
  }

  if (grants.value.length === 0) {
    error.value = '请至少选择一种 grant 类型'
    return
  }

  if (refreshTokenTtlDays.value < 1) {
    error.value = 'refresh_token 有效期至少 1 天'
    return
  }

  isLoading.value = true
  try {
    const response = await api.put(`/oauth/clients/${client.id}`, {
      name: name.value,
      redirect_uris: uris,
      grants: grants.value,
      allowed_scopes: allowedScopes.value,
      auto_consent: autoConsent.value,
      refresh_token_ttl_seconds: refreshTokenTtlDays.value * 86400,
      listed: listed.value,
      logo_url: logoUrl.value,
      tagline: tagline.value,
      display_order: displayOrder.value ?? 0,
    })
    if (response.code === 0) {
      emit('updated')
    } else {
      error.value = response.message || '更新失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" size="lg" aria-label="编辑客户端">
    <div class="space-y-4">
      <h2 class="text-xl font-bold text-foreground">编辑客户端</h2>

      <div class="rounded-lg bg-default-50 p-3">
        <p class="text-xs text-default-400">Client ID</p>
        <p class="mt-1 truncate font-mono text-sm text-foreground">{{ client?.id }}</p>
        <p class="mt-2 text-xs text-default-400">
          类型：{{ isPublicReadonly ? '公共客户端 (SPA / native)' : '机密客户端 (confidential)' }}
          <span class="text-default-300">— 创建后不可修改</span>
        </p>
      </div>

      <KunInput
        v-model="name"
        label="客户端名称"
        placeholder="客户端名称"
        required
      />

      <div>
        <span class="mb-1 block text-sm font-medium text-default-500">回调地址</span>
        <div class="space-y-2">
          <div v-for="(_, index) in redirectUris" :key="index" class="flex gap-2">
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
          variant="light"
          color="primary"
          size="sm"
          class-name="mt-2"
          @click="addUri"
        >
          <KunIcon name="lucide:plus" class="mr-1 size-3" />
          添加回调地址
        </KunButton>
      </div>

      <div>
        <span class="mb-1 block text-sm font-medium text-default-500">
          授权类型 (grants)
          <span class="text-xs text-default-400">— refresh_token 必须勾选，否则 15 分钟后用户会被强制重新登录</span>
        </span>
        <div class="flex flex-wrap gap-2">
          <KunCheckBox
            v-for="g in ALL_GRANTS"
            :key="g"
            :model-value="grants.includes(g)"
            :label="g"
            color="primary"
            class-name="rounded-lg border border-default-200 bg-content1 px-3 py-1.5 hover:border-primary"
            @update:model-value="toggleGrant(g)"
          />
        </div>
      </div>

      <div>
        <span class="mb-1 block text-sm font-medium text-default-500">
          允许的 scope (allowed_scopes)
          <span class="text-xs text-default-400">— image:upload / artifact:upload 这类敏感 scope 必须显式勾选（仅 ren 可授予）</span>
        </span>
        <div class="flex flex-wrap gap-2">
          <KunCheckBox
            v-for="s in scopeOptions"
            :key="s"
            :model-value="allowedScopes.includes(s)"
            :label="s"
            color="primary"
            class-name="rounded-lg border border-default-200 bg-content1 px-3 py-1.5 hover:border-primary"
            @update:model-value="toggleScope(s)"
          />
        </div>
      </div>

      <div>
        <span class="mb-1 block text-sm font-medium text-default-500">
          refresh_token 有效期（天）
          <span class="text-xs text-default-400">— 改动仅影响后续新签发的 token；现有 session 仍按旧 TTL</span>
        </span>
        <KunNumberInput
          v-model="refreshTokenTtlDays"
          :min="1"
          :max="3650"
        />
      </div>

      <div class="space-y-3 rounded-lg border border-default-200 p-3">
        <div>
          <KunSwitch v-model="listed" label="在应用目录中展示 (listed)" />
          <p class="mt-1 text-xs text-default-400">
            — 开启后此应用会出现在注册/登录页的「一键登录」生态 strip（公开）。内部 / 管理类客户端请保持关闭
          </p>
        </div>
        <div class="space-y-1">
          <span class="block text-sm font-medium text-default-500">Logo / 图标</span>
          <KunUpload
            :key="logoUploadKey"
            :initial-image="logoUrl"
            :aspect="1"
            :size="256"
            class-name="w-32"
            description="上传图标"
            @set-image="onLogoPicked"
          />
          <p v-if="logoUploading" class="flex items-center gap-1 text-xs text-default-400">
            <KunIcon name="lucide:loader-circle" class="size-3 animate-spin" />
            上传中…
          </p>
          <p v-else-if="logoError" class="text-xs text-danger-600">{{ logoError }}</p>
        </div>
        <KunInput
          v-model="tagline"
          label="一句话简介 (tagline)"
          placeholder="世界上最萌的 Galgame 论坛"
        />
        <KunNumberInput
          v-if="isRen"
          v-model="displayOrder"
          label="排序 (display_order)"
          :min="0"
          description="小在前，相同再按名称排序（仅 ren 可设置）"
        />
      </div>

      <div v-if="isRen" class="rounded-lg border border-warning-200 bg-warning-50 p-3">
        <div class="flex items-center gap-2 text-sm">
          <KunCheckBox
            v-model="autoConsent"
            label="自动同意 (第一方应用专用)"
            color="warning"
          />
        </div>
        <p class="mt-2 text-xs text-warning-700">
          ⚠️ 勾选后此应用的用户在 OAuth 授权页将
          <strong>跳过手动"同意"步骤</strong>，直接静默授权。
          <strong class="text-danger">仅用于你完全信任的第一方应用</strong>。
          切换会在下次 /oauth/authorize 访问立即生效，不影响已签发的 token。
        </p>
      </div>

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :disabled="isLoading" @click="handleSubmit">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          保存
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
