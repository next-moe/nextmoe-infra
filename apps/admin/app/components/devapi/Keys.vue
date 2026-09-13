<script setup lang="ts">
import {
  DEV_KEY_PAGE_SIZE,
  DEV_KEY_STATE_COLORS,
  DEV_KEY_STATE_LABELS,
  DEV_KEY_STATE_TABS,
  devKeyDeleteMessage,
} from '~/constants/devapi'
import type {
  DevAdminKey,
  DevAdminKeyList,
  DevApp,
  DevKeyMinted,
} from '~~/shared/types/devapi'

const api = useApi()

const state = ref('all')
const clientId = ref('')
const currentPage = ref(1)

const { data, status, refresh } = await useApiFetch<DevAdminKeyList>(
  '/admin/devapi/keys',
  {
    query: computed(() => ({
      state: state.value,
      page: currentPage.value,
      limit: DEV_KEY_PAGE_SIZE,
      ...(clientId.value ? { client_id: clientId.value } : {}),
    })),
  }
)
const { data: appsData } = await useApiFetch<DevApp[]>(
  '/admin/devapi/apps?status=all'
)

const keys = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const isLoading = computed(() => status.value === 'pending')
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / DEV_KEY_PAGE_SIZE))
)

const appOptions = computed(() => [
  { value: '', label: '全部应用' },
  ...(appsData.value ?? []).map((a) => ({
    value: a.client_id,
    label: `${a.name}（${a.client_id.slice(0, 8)}…）`,
  })),
])

watch([state, clientId], () => {
  currentPage.value = 1
})

const ownerLabel = (key: DevAdminKey) =>
  key.owner_user_id ? `归属 #${key.owner_user_id}` : '一方应用'

const busy = ref(false)
const mintedKey = ref<DevKeyMinted | null>(null)
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

const askRotate = (key: DevAdminKey) => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '轮换密钥',
    body: `将为「${key.app_name}」的「${key.name}」生成一枚新密钥，旧密钥进入 72 小时宽限后失效。`,
    run: async () => {
      const res = await api.post<DevKeyMinted>(
        `/admin/devapi/apps/${key.client_id}/keys/${key.id}/rotate`
      )
      if (res.code === 0 && res.data) {
        mintedKey.value = res.data
        revealOpen.value = true
        refresh()
      } else {
        useKunMessage(res.message || '轮换失败', 'error')
      }
    },
  }
}

const askRevoke = (key: DevAdminKey) => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '吊销密钥',
    body: `吊销「${key.app_name}」的「${key.name}」后立即生效且不可撤销。确认继续？`,
    danger: true,
    run: async () => {
      const res = await api.post(
        `/admin/devapi/apps/${key.client_id}/keys/${key.id}/revoke`
      )
      if (res.code === 0) {
        useKunMessage('密钥已吊销', 'success')
        refresh()
      } else {
        useKunMessage(res.message || '吊销失败', 'error')
      }
    },
  }
}

const isDeletable = (key: DevAdminKey) => !!key.revoked_at && !key.last_used_at

const askDelete = (key: DevAdminKey) => {
  confirmOpen.value = true
  confirmDialog.value = {
    title: '删除密钥',
    body: `删除会抹掉「${key.app_name}」的「${key.name}」这条密钥记录本身，不可恢复。只有已吊销且从未被使用过的密钥能删；服务过请求的密钥只能吊销，删不掉。确认继续？`,
    danger: true,
    run: async () => {
      const res = await api.delete(
        `/admin/devapi/apps/${key.client_id}/keys/${key.id}`
      )
      if (res.code === 0) {
        useKunMessage('密钥已删除', 'success')
      } else {
        useKunMessage(devKeyDeleteMessage(key, res.code, res.message), 'error')
      }
      refresh()
    },
  }
}
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-bold text-foreground">全部密钥</h1>
      <p class="mt-1 text-default-500">
        跨全部开发者应用的密钥清单（只展示前缀与后四位，明文只在铸造时出现一次）
      </p>
    </div>

    <div class="flex flex-wrap items-end gap-3">
      <div class="flex flex-wrap gap-2">
        <KunButton
          v-for="tab in DEV_KEY_STATE_TABS"
          :key="tab.id"
          :color="state === tab.id ? 'primary' : 'default'"
          :variant="state === tab.id ? 'solid' : 'flat'"
          size="sm"
          @click="state = tab.id"
        >
          <KunIcon :name="tab.icon" class="mr-1 size-4" />
          {{ tab.label }}
        </KunButton>
      </div>
      <KunSelect
        v-model="clientId"
        class="min-w-64"
        label="应用"
        :options="appOptions"
      />
      <span class="ml-auto text-sm text-default-400">共 {{ total }} 枚密钥</span>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="size-8 animate-spin text-primary"
      />
    </div>

    <div
      v-else-if="!keys.length"
      class="rounded-xl bg-content1 py-12 text-center shadow-sm"
    >
      <KunIcon
        name="lucide:key-round"
        class="mx-auto mb-4 size-12 text-default-200"
      />
      <p class="text-default-400">没有符合条件的密钥</p>
    </div>

    <div v-else class="space-y-3">
      <KunCard
        v-for="k in keys"
        :key="k.id"
        content-class="justify-start gap-0 items-stretch"
        class-name="p-4"
      >
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-medium text-foreground">{{ k.name }}</span>
              <KunChip
                :color="DEV_KEY_STATE_COLORS[k.state] ?? 'default'"
                variant="flat"
                size="xs"
              >
                {{ DEV_KEY_STATE_LABELS[k.state] ?? k.state }}
              </KunChip>
              <NuxtLink
                :to="`/devapi/${k.client_id}`"
                class="text-sm text-primary hover:underline"
              >
                {{ k.app_name }}
              </NuxtLink>
              <span class="text-xs text-default-400">{{ ownerLabel(k) }}</span>
            </div>
            <p class="mt-1 font-mono text-sm text-default-500">
              {{ k.key_prefix }}…{{ k.last4 }}
            </p>
            <div class="mt-2 flex flex-wrap gap-1">
              <KunChip
                v-for="s in k.scopes"
                :key="s"
                color="default"
                variant="flat"
                size="xs"
              >
                {{ s }}
              </KunChip>
            </div>
          </div>

          <div class="flex shrink-0 gap-1">
            <KunButton
              variant="flat"
              size="sm"
              :disabled="busy || k.state === 'revoked'"
              @click="askRotate(k)"
            >
              <KunIcon name="lucide:refresh-cw" class="mr-1 size-4" />
              轮换
            </KunButton>
            <KunButton
              color="danger"
              variant="flat"
              size="sm"
              :disabled="busy || k.state === 'revoked'"
              @click="askRevoke(k)"
            >
              <KunIcon name="lucide:ban" class="mr-1 size-4" />
              吊销
            </KunButton>
            <KunButton
              v-if="isDeletable(k)"
              color="danger"
              size="sm"
              :disabled="busy"
              @click="askDelete(k)"
            >
              <KunIcon name="lucide:trash-2" class="mr-1 size-4" />
              删除
            </KunButton>
          </div>
        </div>

        <div class="mt-3 grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
          <div>
            <p class="text-default-400">创建</p>
            <p class="mt-0.5 text-default-500">
              {{ formatDate(k.created_at, { isShowYear: true, isPrecise: true }) }}
            </p>
          </div>
          <div>
            <p class="text-default-400">最近使用</p>
            <p class="mt-0.5 text-default-500">
              {{
                k.last_used_at
                  ? formatDate(k.last_used_at, { isShowYear: true, isPrecise: true })
                  : '从未'
              }}
            </p>
          </div>
          <div v-if="k.expires_at">
            <p class="text-default-400">失效时间</p>
            <p class="mt-0.5 text-default-500">
              {{ formatDate(k.expires_at, { isShowYear: true, isPrecise: true }) }}
            </p>
          </div>
          <div v-if="k.revoked_at">
            <p class="text-default-400">吊销时间</p>
            <p class="mt-0.5 text-danger-600">
              {{ formatDate(k.revoked_at, { isShowYear: true, isPrecise: true }) }}
            </p>
          </div>
        </div>
      </KunCard>
    </div>

    <div v-if="totalPages > 1" class="flex justify-center">
      <KunPagination
        v-model:current-page="currentPage"
        :total-page="totalPages"
        :is-loading="isLoading"
      />
    </div>

    <DevapiKeyRevealModal
      v-model:open="revealOpen"
      :minted="mintedKey"
      :rotated="true"
    />

    <KunModal
      v-model="confirmOpen"
      role="alertdialog"
      :aria-label="confirmDialog?.title ?? '确认'"
    >
      <div class="space-y-4">
        <h2 class="text-xl font-bold text-foreground">
          {{ confirmDialog?.title }}
        </h2>
        <p class="text-sm text-default-500">{{ confirmDialog?.body }}</p>
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
            :color="confirmDialog?.danger ? 'danger' : 'primary'"
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
