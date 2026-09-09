<script setup lang="ts">
import {
  API_CODE_VALIDATION_FAILED,
  DEV_APP_REVIEW_COLORS,
  DEV_APP_REVIEW_LABELS,
  DEV_CAP_APP_MANAGE,
  DEV_CAP_KEY_MINT,
  DEV_DISABLED_HINT,
  DEV_TIER_COLORS,
  DEV_TIER_LABELS,
  isAppUnderReview
} from '~/constants/dev'
import type {
  DevApp,
  DevKey,
  DevKeyMinted,
  DevPolicies
} from '~~/shared/types/dev'

const route = useRoute()
const clientId = computed(() => String(route.params.clientId))

useSeoMeta({ title: '应用详情', robots: 'noindex' })

const api = useApi()

const { data: appData, refresh: refreshApp } = await useApiFetch<DevApp>(
  () => `/dev/apps/${clientId.value}`
)
const { data: keysData, refresh: refreshKeys } = await useApiFetch<DevKey[]>(
  () => `/dev/apps/${clientId.value}/keys`
)
const { data: policiesData } = await useApiFetch<DevPolicies>('/dev/policies')

const app = computed(() => appData.value)
const keys = computed(() => keysData.value ?? [])

const policy = (capability: string) =>
  policiesData.value?.[capability] ?? 'self_service'

const manageDisabled = computed(() => policy(DEV_CAP_APP_MANAGE) === 'disabled')
const mintDisabled = computed(() => policy(DEV_CAP_KEY_MINT) === 'disabled')

const reviewStatus = computed(() => app.value?.review_status ?? '')
const underReview = computed(() => isAppUnderReview(reviewStatus.value))
const isDeclined = computed(() => reviewStatus.value === 'declined')
const reviewChip = computed(() => {
  if (!underReview.value) return null
  return {
    label: DEV_APP_REVIEW_LABELS[reviewStatus.value],
    color: DEV_APP_REVIEW_COLORS[reviewStatus.value]
  }
})

const busy = ref(false)
const showEditModal = ref(false)
const showUserLoginModal = ref(false)
const showMintModal = ref(false)

const showReveal = ref(false)
const mintedKey = ref<DevKeyMinted | null>(null)
const revealRotated = ref(false)

const reveal = (minted: DevKeyMinted, rotated: boolean) => {
  mintedKey.value = minted
  revealRotated.value = rotated
  showReveal.value = true
}

interface ConfirmDialog {
  title: string
  body: string
  danger?: boolean
  run: () => Promise<void>
}

// The payload outlives the close so the panel still has something to render
// while KunModal's leave transition plays — nulling it on close emptied the
// panel for the length of the animation.
const confirmOpen = ref(false)
const confirmDialog = ref<ConfirmDialog | null>(null)

const askConfirm = (dialog: ConfirmDialog) => {
  confirmDialog.value = dialog
  confirmOpen.value = true
}

const runConfirm = async () => {
  if (!confirmDialog.value) return
  busy.value = true
  try {
    await confirmDialog.value.run()
  } finally {
    busy.value = false
    confirmOpen.value = false
  }
}

const limitLabel = (v: number, unit: string) =>
  v > 0 ? `${v.toLocaleString()} ${unit}` : '不限'

const handleMinted = (minted: DevKeyMinted) => {
  reveal(minted, false)
  refreshKeys()
  refreshApp()
}

const handleEdited = () => {
  refreshApp()
}

const askRotate = (key: DevKey) => {
  askConfirm({
    title: '轮换密钥',
    body: `将为「${key.name}」生成一枚新密钥，旧密钥进入 72 小时宽限后失效。请确保有机会保存新密钥。`,
    run: async () => {
      const res = await api.post<DevKeyMinted>(
        `/dev/apps/${clientId.value}/keys/${key.id}/rotate`
      )
      if (res.code === 0 && res.data) {
        reveal(res.data, true)
        refreshKeys()
      } else {
        useKunMessage(res.message || '轮换失败', 'error')
      }
    }
  })
}

const askRevoke = (key: DevKey) => {
  askConfirm({
    title: '吊销密钥',
    body: `吊销「${key.name}」后立即生效且不可撤销，使用该密钥的所有请求将被拒绝。确认继续？`,
    danger: true,
    run: async () => {
      const res = await api.post(
        `/dev/apps/${clientId.value}/keys/${key.id}/revoke`
      )
      if (res.code === 0) {
        useKunMessage('密钥已吊销', 'success')
        refreshKeys()
        refreshApp()
      } else {
        useKunMessage(res.message || '吊销失败', 'error')
      }
    }
  })
}

interface KeyDeleteFailure {
  message: string
  refresh?: boolean
}

const deleteKeyFailure = (
  key: DevKey,
  res: { code: number; message: string }
): KeyDeleteFailure => {
  if (key.last_used_at) {
    return { message: '该密钥已产生调用记录，只能吊销，不能删除' }
  }
  if (!key.revoked_at) return { message: '请先吊销该密钥，再执行删除' }
  if (res.code === API_CODE_VALIDATION_FAILED) {
    return { message: '该密钥当前不可删除，已为你刷新列表', refresh: true }
  }
  return { message: res.message || '删除失败' }
}

const askDeleteKey = (key: DevKey) => {
  askConfirm({
    title: '删除密钥',
    body: `将「${key.name}」的记录从列表中彻底删除。它已被吊销且从未产生过调用，删除后不可恢复。`,
    danger: true,
    run: async () => {
      const res = await api.delete(`/dev/apps/${clientId.value}/keys/${key.id}`)
      if (res.code === 0) {
        useKunMessage('密钥已删除', 'success')
        refreshKeys()
      } else {
        const failure = deleteKeyFailure(key, res)
        if (failure.refresh) refreshKeys()
        useKunMessage(failure.message, 'error')
      }
    }
  })
}

const resubmitting = ref(false)

const resubmit = async () => {
  resubmitting.value = true
  try {
    const res = await api.post<DevApp>(`/dev/apps/${clientId.value}/resubmit`)
    if (res.code === 0) {
      useKunMessage('已重新提交，等待平台审核', 'success')
      refreshApp()
    } else {
      useKunMessage(res.message || '提交失败', 'error')
    }
  } finally {
    resubmitting.value = false
  }
}

const deleteAction = computed(() => {
  const withdraw = reviewStatus.value === 'pending'
  const label = withdraw ? '撤回申请' : '删除应用'
  return {
    label,
    verb: withdraw ? '撤回' : '删除',
    done: withdraw ? '申请已撤回' : '应用已删除'
  }
})

const askDeleteApp = () => {
  const { label, verb, done } = deleteAction.value
  askConfirm({
    title: label,
    body: `${verb}后，该应用的所有密钥立即吊销，应用从你的列表中消失，占用的名额也随之归还，可用来创建新的应用。此操作你无法自行撤销，请确认后再继续。`,
    danger: true,
    run: async () => {
      const res = await api.delete(`/dev/apps/${clientId.value}`)
      if (res.code === 0) {
        useKunMessage(done, 'success')
        navigateTo('/dashboard')
      } else {
        useKunMessage(res.message || `${verb}失败`, 'error')
      }
    }
  })
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center gap-2">
      <KunButton
        variant="light"
        size="sm"
        is-icon-only
        aria-label="返回控制台"
        @click="navigateTo('/dashboard')"
      >
        <KunIcon name="lucide:arrow-left" class="size-5" />
      </KunButton>
      <h1 class="text-foreground text-2xl font-bold">应用详情</h1>
    </div>

    <KunCard v-if="!app" content-class="p-10">
      <p class="text-default-400 text-center">
        未找到该应用（可能已删除或不存在）。
      </p>
    </KunCard>

    <template v-else>
      <KunCard content-class="justify-start gap-0" class-name="p-6">
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="text-foreground text-lg font-semibold">
                {{ app.name }}
              </h2>
              <KunChip
                :color="DEV_TIER_COLORS[app.tier] ?? 'default'"
                variant="flat"
                size="sm"
              >
                {{ DEV_TIER_LABELS[app.tier] ?? app.tier }}
              </KunChip>
              <KunChip
                v-if="reviewChip"
                :color="reviewChip.color"
                variant="flat"
                size="sm"
              >
                {{ reviewChip.label }}
              </KunChip>
            </div>
            <p v-if="app.description" class="text-default-500 mt-1 text-sm">
              {{ app.description }}
            </p>
            <div class="mt-2 flex items-center gap-2">
              <p class="text-default-400 truncate font-mono text-sm">
                {{ app.client_id }}
              </p>
              <KunCopy :text="app.client_id" size="sm" />
            </div>
          </div>
          <div class="flex shrink-0 gap-1">
            <KunButton
              v-if="isDeclined"
              color="primary"
              size="sm"
              :disabled="resubmitting"
              @click="resubmit"
            >
              <KunIcon
                v-if="resubmitting"
                name="lucide:loader-circle"
                class="mr-1 size-4 animate-spin"
              />
              <KunIcon v-else name="lucide:send" class="mr-1 size-4" />
              重新提交
            </KunButton>
            <KunButton
              variant="flat"
              size="sm"
              :disabled="manageDisabled"
              @click="showEditModal = true"
            >
              <KunIcon name="lucide:pencil" class="mr-1 size-4" />
              编辑
            </KunButton>
            <KunButton
              color="danger"
              variant="flat"
              size="sm"
              :disabled="manageDisabled"
              @click="askDeleteApp"
            >
              <KunIcon name="lucide:trash-2" class="mr-1 size-4" />
              {{ deleteAction.label }}
            </KunButton>
          </div>
        </div>

        <p
          v-if="isDeclined && app.review_note"
          class="bg-danger-50 text-danger mt-3 rounded-lg p-3 text-sm"
        >
          未通过审核：{{ app.review_note }}
        </p>
        <p
          v-else-if="reviewStatus === 'pending'"
          class="bg-warning-50 text-warning mt-3 rounded-lg p-3 text-sm"
        >
          已提交，等待平台审核。通过后应用即启用，届时可铸造密钥。
        </p>
        <p
          v-else-if="manageDisabled"
          class="bg-default-100 text-default-500 mt-3 rounded-lg p-3 text-sm"
        >
          {{ DEV_DISABLED_HINT }}：应用的编辑与删除暂由平台代为处理。
        </p>

        <div class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div>
            <p class="text-default-400 text-xs">分层</p>
            <p class="text-foreground mt-0.5 text-sm">
              {{ DEV_TIER_LABELS[app.tier] ?? app.tier }}
            </p>
          </div>
          <div>
            <p class="text-default-400 text-xs">限流</p>
            <p class="text-foreground mt-0.5 text-sm">
              {{ limitLabel(app.rate_per_min, '次/分') }}
            </p>
          </div>
          <div>
            <p class="text-default-400 text-xs">日配额</p>
            <p class="text-foreground mt-0.5 text-sm">
              {{ limitLabel(app.quota_daily, '次/日') }}
            </p>
          </div>
          <div>
            <p class="text-default-400 text-xs">创建时间</p>
            <p class="text-foreground mt-0.5 text-sm">
              {{ formatDate(app.created_at, { isShowYear: true }) }}
            </p>
          </div>
        </div>
      </KunCard>

      <div class="flex items-center justify-between">
        <h2 class="text-foreground text-lg font-semibold">用户登录</h2>
        <KunButton
          :color="app.user_login ? 'default' : 'primary'"
          variant="flat"
          size="sm"
          :disabled="manageDisabled"
          @click="showUserLoginModal = true"
        >
          <KunIcon
            :name="app.user_login ? 'lucide:pencil' : 'lucide:log-in'"
            class="mr-1 size-4"
          />
          {{ app.user_login ? '编辑' : '开启用户登录' }}
        </KunButton>
      </div>

      <KunCard content-class="justify-start gap-0" class-name="p-6">
        <div v-if="app.user_login" class="space-y-4">
          <div>
            <p class="text-default-400 text-xs">回调地址</p>
            <div class="mt-1 space-y-1">
              <div
                v-for="uri in app.user_login.redirect_uris"
                :key="uri"
                class="flex min-w-0 items-center gap-2"
              >
                <p class="text-foreground truncate font-mono text-sm">
                  {{ uri }}
                </p>
                <KunCopy :text="uri" size="sm" />
              </div>
            </div>
          </div>
          <div>
            <p class="text-default-400 text-xs">同意 scope</p>
            <div class="mt-1 flex flex-wrap gap-2">
              <KunChip
                v-for="scope in app.user_login.scopes"
                :key="scope"
                variant="flat"
                size="sm"
              >
                {{ scope }}
              </KunChip>
            </div>
          </div>
          <p class="text-default-500 text-xs">
            PKCE 强制（S256）：授权请求必须携带 code_challenge。
          </p>
        </div>
        <p v-else class="text-default-500 text-sm">
          开启后，应用可通过 OAuth 授权码 + PKCE 流程让 NextMoe
          用户登录；回调地址与 scope 在此自助配置，无需联系平台。
        </p>
      </KunCard>

      <div class="flex items-center justify-between">
        <h2 class="text-foreground text-lg font-semibold">API 密钥</h2>
        <KunButton
          color="primary"
          size="sm"
          :disabled="mintDisabled || underReview"
          @click="showMintModal = true"
        >
          <KunIcon name="lucide:plus" class="mr-1 size-4" />
          生成新密钥
        </KunButton>
      </div>

      <p
        v-if="underReview"
        class="bg-default-100 text-default-500 rounded-lg p-3 text-sm"
      >
        审核通过后可铸造密钥。
      </p>
      <p
        v-else-if="mintDisabled"
        class="bg-default-100 text-default-500 rounded-lg p-3 text-sm"
      >
        {{ DEV_DISABLED_HINT }}：暂不能铸造或轮换密钥；已有密钥仍可随时吊销。
      </p>

      <KeysTable
        :keys="keys"
        :busy="busy"
        :rotate-disabled="mintDisabled"
        @rotate="askRotate"
        @revoke="askRevoke"
        @delete="askDeleteKey"
      />

      <DashboardAppsUsage :client-id="clientId" />
    </template>

    <DashboardAppsEditModal
      v-if="app"
      v-model:open="showEditModal"
      :app="app"
      @updated="handleEdited"
    />

    <DashboardAppsUserLoginModal
      v-if="app"
      v-model:open="showUserLoginModal"
      :app="app"
      @updated="handleEdited"
    />

    <KeysMintModal
      v-model:open="showMintModal"
      :client-id="clientId"
      @minted="handleMinted"
    />

    <KeysRevealModal
      v-model:open="showReveal"
      :minted="mintedKey"
      :rotated="revealRotated"
    />

    <KunModal
      v-model="confirmOpen"
      role="alertdialog"
      :aria-label="confirmDialog?.title ?? '确认'"
    >
      <div v-if="confirmDialog" class="space-y-4">
        <h2 class="text-foreground text-xl font-bold">
          {{ confirmDialog.title }}
        </h2>
        <p class="text-default-500 text-sm">{{ confirmDialog.body }}</p>
        <div class="flex justify-end gap-3">
          <KunButton
            color="default"
            variant="flat"
            :disabled="busy"
            @click="confirmOpen = false"
          >
            取消
          </KunButton>
          <KunButton
            :color="confirmDialog.danger ? 'danger' : 'primary'"
            :disabled="busy"
            @click="runConfirm"
          >
            <KunIcon
              v-if="busy"
              name="lucide:loader-circle"
              class="mr-2 size-4 animate-spin"
            />
            确认
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
