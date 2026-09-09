<script setup lang="ts">
import { ALL_GRANTS, KNOWN_SCOPES, REN_ONLY_SCOPES, DEFAULT_REFRESH_TOKEN_TTL_SECONDS } from '~~/shared/types/oauth-client'

const show = defineModel<boolean>({ required: true })
const props = defineProps<{ sites: Site[] }>()
const emit = defineEmits<{ created: [client: OAuthClientCreated] }>()

const api = useApi()

const { isRen } = useAuth()
const scopeOptions = computed(() =>
  isRen.value ? KNOWN_SCOPES : KNOWN_SCOPES.filter((s) => !REN_ONLY_SCOPES.includes(s))
)

const siteId = ref<number | ''>('')
const name = ref('')
const redirectUris = ref([''])
const grants = ref<string[]>(['authorization_code', 'refresh_token'])
const allowedScopes = ref<string[]>(['openid', 'profile', 'email'])
const isPublic = ref(false)
// First-party client — silently skips /oauth/authorize consent UI for
const autoConsent = ref(false)
const refreshTokenTtlDays = ref(DEFAULT_REFRESH_TOKEN_TTL_SECONDS / 86400)
const listed = ref(false)
const logoUrl = ref('')
const tagline = ref('')
const displayOrder = ref<number | null>(0)
const error = ref('')
const isLoading = ref(false)

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

const siteOptions = computed(() =>
  props.sites.map((s) => ({ value: s.id, label: `${s.name} (${s.domain})` }))
)

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
  error.value = ''

  if (!siteId.value || !name.value) {
    error.value = '请选择站点并填写名称'
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
    const response = await api.post<OAuthClientCreated>('/oauth/clients', {
      site_id: Number(siteId.value),
      name: name.value,
      redirect_uris: uris,
      grants: grants.value,
      allowed_scopes: allowedScopes.value,
      is_public: isPublic.value,
      auto_consent: autoConsent.value,
      refresh_token_ttl_seconds: refreshTokenTtlDays.value * 86400,
      listed: listed.value,
      logo_url: logoUrl.value,
      tagline: tagline.value,
      display_order: displayOrder.value ?? 0,
    })
    if (response.code === 0) {
      emit('created', response.data)
      siteId.value = ''
      name.value = ''
      redirectUris.value = ['']
      grants.value = ['authorization_code', 'refresh_token']
      allowedScopes.value = ['openid', 'profile', 'email']
      isPublic.value = false
      autoConsent.value = false
      refreshTokenTtlDays.value = DEFAULT_REFRESH_TOKEN_TTL_SECONDS / 86400
      listed.value = false
      logoUrl.value = ''
      tagline.value = ''
      displayOrder.value = 0
      logoUploadKey.value++
    } else {
      error.value = response.message || '创建失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="show" size="lg" aria-label="创建 OAuth 客户端">
    <div class="space-y-4">
      <h2 class="text-xl font-bold text-foreground">创建 OAuth 客户端</h2>

      <KunSelect
        v-model="siteId"
        label="关联站点"
        placeholder="请选择站点"
        :options="siteOptions"
      />

      <KunInput
        v-model="name"
        label="客户端名称"
        placeholder="例如：KUN Galgame Web"
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
          <span class="text-xs text-default-400">— 默认 90 天；用户登录后无感续期的最长窗口</span>
        </span>
        <KunNumberInput
          v-model="refreshTokenTtlDays"
          :min="1"
          :max="3650"
        />
        <p class="mt-1 text-xs text-default-400">
          常见取值：1（高敏感后台）/ 7 / 30 / <strong>90（默认）</strong> / 365（长寿后台服务）
        </p>
      </div>

      <div class="flex items-center gap-2 text-sm">
        <KunCheckBox
          v-model="isPublic"
          label="公共客户端 (SPA / native)"
          color="primary"
        />
        <span class="text-xs text-default-400">— PKCE 必须；refresh 不需要 client_secret</span>
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
          <strong class="text-danger">仅用于你完全信任的第一方应用</strong>
          （如鲲 Galgame 论坛 / 补丁 / Wiki）；任何第三方应用必须保持默认关闭。
        </p>
      </div>

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="show = false">
          取消
        </KunButton>
        <KunButton color="primary" :disabled="isLoading" @click="handleSubmit">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          创建
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
