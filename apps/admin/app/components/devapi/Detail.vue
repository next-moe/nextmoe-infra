<script setup lang="ts">
import {
  DEV_TIER_COLORS,
  devAppDeleteBlockedReason,
  devAppDeleteMessage,
  devKeyDeleteMessage,
  devTierLimitHint,
} from '~/constants/devapi'
import type { DevApp, DevKey, DevKeyMinted } from '~~/shared/types/devapi'

const route = useRoute()
const clientId = computed(() => String(route.params.clientId))

const api = useApi()

// status=all, not the default: an archived application is the only one that can
// be deleted, and every other filter hides it — the detail page would answer
// "未找到该应用" for exactly the row the delete step exists for.
const { data: appsData, refresh: refreshApps } = await useApiFetch<DevApp[]>(
  '/admin/devapi/apps?status=all'
)
const { data: keysData, refresh: refreshKeys } = await useApiFetch<DevKey[]>(
  () => `/admin/devapi/apps/${clientId.value}/keys`
)

const app = computed(() =>
  (appsData.value ?? []).find((a) => a.client_id === clientId.value) ?? null
)
const keys = computed(() => keysData.value ?? [])
const isArchived = computed(() => !!app.value?.archived_at)
const deleteBlockedReason = computed(() => devAppDeleteBlockedReason(app.value))

const busy = ref(false)
const showConfigModal = ref(false)
const showMintModal = ref(false)

const mintedKey = ref<DevKeyMinted | null>(null)
const revealRotated = ref(false)
const revealOpen = ref(false)

const confirmDialog = ref<{
  title: string
  body: string
  danger?: boolean
  run: () => Promise<void>
} | null>(null)
const confirmOpen = ref(false)

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

const handleMinted = (minted: DevKeyMinted) => {
  showMintModal.value = false
  revealRotated.value = false
  mintedKey.value = minted
  revealOpen.value = true
  refreshKeys()
}

const askRotate = (key: DevKey) => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '轮换密钥',
    body: `将为「${key.name}」生成一枚新密钥，旧密钥进入 72 小时宽限后失效。请确保有机会保存新密钥。`,
    run: async () => {
      const res = await api.post<DevKeyMinted>(
        `/admin/devapi/apps/${clientId.value}/keys/${key.id}/rotate`
      )
      if (res.code === 0 && res.data) {
        revealRotated.value = true
        mintedKey.value = res.data
        revealOpen.value = true
        refreshKeys()
      } else {
        useKunMessage(res.message || '轮换失败', 'error')
      }
    },
  }
}

const askRevoke = (key: DevKey) => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '吊销密钥',
    body: `吊销「${key.name}」后立即生效且不可撤销，使用该密钥的所有请求将被拒绝。确认继续？`,
    danger: true,
    run: async () => {
      const res = await api.post(
        `/admin/devapi/apps/${clientId.value}/keys/${key.id}/revoke`
      )
      if (res.code === 0) {
        useKunMessage('密钥已吊销', 'success')
        refreshKeys()
      } else {
        useKunMessage(res.message || '吊销失败', 'error')
      }
    },
  }
}

const askDeleteKey = (key: DevKey) => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '删除密钥',
    body: `删除会抹掉「${key.name}」这条密钥记录本身，不可恢复。只有已吊销且从未被使用过的密钥能删；服务过请求的密钥只能吊销，删不掉。确认继续？`,
    danger: true,
    run: async () => {
      const res = await api.delete(
        `/admin/devapi/apps/${clientId.value}/keys/${key.id}`
      )
      if (res.code === 0) {
        useKunMessage('密钥已删除', 'success')
      } else {
        useKunMessage(devKeyDeleteMessage(key, res.code, res.message), 'error')
      }
      refreshKeys()
    },
  }
}

const askDisable = () => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '停用应用',
    body: '停用后该应用退出开放 API 平台，其所有密钥将立即失效。可随时重新启用。确认继续？',
    danger: true,
    run: async () => {
      const res = await api.patch(`/admin/devapi/apps/${clientId.value}`, {
        dev_enabled: false,
      })
      if (res.code === 0) {
        useKunMessage('应用已停用', 'success')
        navigateTo('/devapi')
      } else {
        useKunMessage(res.message || '停用失败', 'error')
      }
    },
  }
}

const askArchive = () => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '归档应用',
    body: '归档后，该应用的所有密钥立即吊销，应用从归属用户的列表中消失、归还其 5 个应用名额中的一个，DLsite 结算资格也一并清除。重新启用即可撤销归档。确认继续？',
    danger: true,
    run: async () => {
      const res = await api.post<DevApp>(
        `/admin/devapi/apps/${clientId.value}/archive`
      )
      if (res.code === 0) {
        useKunMessage('应用已归档', 'success')
        refreshApps()
        refreshKeys()
      } else {
        useKunMessage(res.message || '归档失败', 'error')
      }
    },
  }
}

const askDeleteApp = () => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '删除应用',
    body: '删除会抹掉应用记录本身，不可恢复。只有归档后再没有任何密钥、调用记录、商店短链与未过期登录会话的空壳应用才能删；只要还有东西挂在它名下，服务端就会拒绝，归档就是删除的极限。确认继续？',
    danger: true,
    run: async () => {
      const res = await api.delete(`/admin/devapi/apps/${clientId.value}`)
      if (res.code === 0) {
        useKunMessage('应用已删除', 'success')
        navigateTo('/devapi')
        return
      }
      useKunMessage(devAppDeleteMessage(app.value, res.code, res.message), 'error')
      refreshApps()
    },
  }
}

const handleConfigUpdated = () => {
  showConfigModal.value = false
  refreshApps()
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center gap-2">
      <KunButton variant="light" size="sm" is-icon-only aria-label="返回" @click="navigateTo('/devapi')">
        <KunIcon name="lucide:arrow-left" class="size-5" />
      </KunButton>
      <h1 class="text-2xl font-bold text-foreground">应用详情</h1>
    </div>

    <KunCard v-if="!app" content-class="p-10">
      <p class="text-center text-default-400">
        未找到该应用（可能已被删除或不存在）。
      </p>
    </KunCard>

    <template v-else>
      <KunCard content-class="justify-start gap-0" class-name="p-6">
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="text-lg font-semibold text-foreground">{{ app.name }}</h2>
              <KunChip :color="DEV_TIER_COLORS[app.dev_tier] ?? 'default'" variant="flat" size="sm">
                {{ app.dev_tier }}
              </KunChip>
              <KunChip v-if="isArchived" color="warning" variant="flat" size="sm">
                已归档
              </KunChip>
            </div>
            <div class="mt-1 flex items-center gap-2">
              <p class="truncate font-mono text-sm text-default-400">{{ app.client_id }}</p>
              <KunCopy :text="app.client_id" size="sm" />
            </div>
          </div>
          <div class="flex shrink-0 gap-1">
            <KunButton variant="flat" size="sm" @click="showConfigModal = true">
              <KunIcon name="lucide:sliders-horizontal" class="mr-1 size-4" />
              编辑配置
            </KunButton>
            <KunButton
              v-if="app.dev_enabled"
              color="danger"
              variant="flat"
              size="sm"
              @click="askDisable"
            >
              <KunIcon name="lucide:power-off" class="mr-1 size-4" />
              停用
            </KunButton>
            <KunButton
              v-if="!isArchived"
              color="danger"
              variant="flat"
              size="sm"
              @click="askArchive"
            >
              <KunIcon name="lucide:archive" class="mr-1 size-4" />
              归档
            </KunButton>
            <div
              v-else-if="deleteBlockedReason"
              class="flex items-center gap-1 self-center px-1 text-xs text-default-400"
            >
              <KunIcon name="lucide:lock" class="size-4" />
              {{ deleteBlockedReason }}
            </div>
            <KunButton v-else color="danger" size="sm" @click="askDeleteApp">
              <KunIcon name="lucide:trash-2" class="mr-1 size-4" />
              删除
            </KunButton>
          </div>
        </div>

        <div class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div>
            <p class="text-xs text-default-400">归属用户</p>
            <p class="mt-0.5 text-sm text-foreground">
              {{ app.owner_user_id ? `#${app.owner_user_id}` : '未指定' }}
            </p>
          </div>
          <div>
            <p class="text-xs text-default-400">限流</p>
            <p class="mt-0.5 text-sm text-foreground">
              {{ app.dev_rate_per_min > 0 ? `${app.dev_rate_per_min} 次/分` : '默认' }}
            </p>
          </div>
          <div>
            <p class="text-xs text-default-400">日配额</p>
            <p class="mt-0.5 text-sm text-foreground">
              {{ app.dev_quota_daily > 0 ? `${app.dev_quota_daily} 次/日` : '默认' }}
            </p>
          </div>
          <div>
            <p class="text-xs text-default-400">Tier 默认限额</p>
            <p class="mt-0.5 text-sm text-foreground">{{ devTierLimitHint(app.dev_tier) }}</p>
          </div>
          <div>
            <p class="text-xs text-default-400">结算资格</p>
            <KunChip
              class-name="mt-0.5"
              :color="app.store_settlement_eligible ? 'success' : 'default'"
              variant="flat"
              size="xs"
            >
              {{ app.store_settlement_eligible ? '参与分成' : '不参与分成' }}
            </KunChip>
          </div>
          <div v-if="app.archived_at">
            <p class="text-xs text-default-400">归档时间</p>
            <p class="mt-0.5 text-sm text-warning">
              {{ formatDate(app.archived_at, { isShowYear: true, isPrecise: true }) }}
            </p>
          </div>
        </div>
      </KunCard>

      <div class="flex items-center justify-between">
        <h2 class="text-lg font-semibold text-foreground">API 密钥</h2>
        <KunButton v-if="!isArchived" color="primary" size="sm" @click="showMintModal = true">
          <KunIcon name="lucide:plus" class="mr-1 size-4" />
          生成新密钥
        </KunButton>
      </div>

      <DevapiKeyTable
        :keys="keys"
        :busy="busy"
        @rotate="askRotate"
        @revoke="askRevoke"
        @delete="askDeleteKey"
      />
    </template>

    <DevapiConfigModal
      v-model:open="showConfigModal"
      :app="app"
      @updated="handleConfigUpdated"
    />

    <DevapiMintModal
      v-model:open="showMintModal"
      :client-id="clientId"
      @minted="handleMinted"
    />

    <DevapiKeyRevealModal
      v-model:open="revealOpen"
      :minted="mintedKey"
      :rotated="revealRotated"
    />

    <KunModal
      v-model="confirmOpen"
      role="alertdialog"
      :aria-label="confirmDialog?.title ?? '确认'"
    >
      <div class="space-y-4">
        <h2 class="text-xl font-bold text-foreground">{{ confirmDialog?.title }}</h2>
        <p class="text-sm text-default-500">{{ confirmDialog?.body }}</p>
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" :disabled="busy" @click="confirmOpen = false">
            取消
          </KunButton>
          <KunButton :color="confirmDialog?.danger ? 'danger' : 'primary'" :disabled="busy" @click="runConfirm">
            <KunIcon v-if="busy" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
            确认
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
