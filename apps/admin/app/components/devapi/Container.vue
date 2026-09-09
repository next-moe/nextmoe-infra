<script setup lang="ts">
import {
  DEV_APP_STATUS_TABS,
  DEV_TIER_COLORS,
  devAppReviewColor,
  devAppReviewLabel,
  devTierLimitHint,
} from '~/constants/devapi'
import type { DevApp } from '~~/shared/types/devapi'

const statusFilter = ref('enabled')

const { data: appsData, status, refresh } = await useApiFetch<DevApp[]>(
  '/admin/devapi/apps',
  { query: computed(() => ({ status: statusFilter.value })) }
)
const { data: clientsData, refresh: refreshClients } =
  await useApiFetch<OAuthClient[]>('/oauth/clients')
// The enable modal offers whatever is not live on the platform right now, so it
// reads the enabled set on its own — deriving it from the filtered list above
// would offer an already-enabled client as a candidate the moment the operator
// switches the filter.
const { data: enabledData, refresh: refreshEnabled } = await useApiFetch<
  DevApp[]
>('/admin/devapi/apps?status=enabled')

const apps = computed(() => appsData.value ?? [])
const clients = computed(() => clientsData.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const enabledIds = computed(
  () => new Set((enabledData.value ?? []).map((a) => a.client_id))
)
const candidates = computed(() =>
  clients.value.filter((c) => !enabledIds.value.has(c.id))
)

const showEnableModal = ref(false)
const editingApp = ref<DevApp | null>(null)
const configOpen = ref(false)

const openConfig = (app: DevApp) => {
  editingApp.value = app
  configOpen.value = true
}

const refreshAll = () => {
  refresh()
  refreshClients()
  refreshEnabled()
}

const handleEnabled = () => {
  showEnableModal.value = false
  refreshAll()
}

const handleUpdated = () => {
  configOpen.value = false
  refreshAll()
}

const ownerLabel = (app: DevApp) =>
  app.owner_user_id ? `#${app.owner_user_id}` : '未指定'

const overrideLabel = (app: DevApp) => {
  const rate = app.dev_rate_per_min > 0 ? `${app.dev_rate_per_min} 次/分` : null
  const quota = app.dev_quota_daily > 0 ? `${app.dev_quota_daily} 次/日` : null
  if (!rate && !quota) return null
  return [rate, quota].filter(Boolean).join(' · ')
}

const emptyHint = computed(() =>
  statusFilter.value === 'enabled'
    ? '尚无已启用的开发者应用'
    : '该状态下没有应用'
)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-foreground">开发者平台</h1>
        <p class="mt-1 text-default-500">管理 NextMoe 开放 API 的应用、等级与密钥</p>
      </div>
      <div class="flex shrink-0 gap-2 self-start">
        <KunButton variant="flat" @click="navigateTo('/devapi/keys')">
          <KunIcon name="lucide:key-round" class="mr-2 size-4" />
          全部密钥
        </KunButton>
        <KunButton color="primary" @click="showEnableModal = true">
          <KunIcon name="lucide:plus" class="mr-2 size-4" />
          启用新应用
        </KunButton>
      </div>
    </div>

    <DevapiPolicyMatrix />

    <div class="flex flex-wrap gap-2">
      <KunButton
        v-for="tab in DEV_APP_STATUS_TABS"
        :key="tab.id"
        :color="statusFilter === tab.id ? 'primary' : 'default'"
        :variant="statusFilter === tab.id ? 'solid' : 'flat'"
        size="sm"
        @click="statusFilter = tab.id"
      >
        <KunIcon :name="tab.icon" class="mr-1 size-4" />
        {{ tab.label }}
      </KunButton>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="size-8 animate-spin text-primary" />
    </div>

    <div v-else-if="apps.length === 0" class="rounded-xl bg-content1 py-12 text-center shadow-sm">
      <KunIcon name="lucide:terminal" class="mx-auto mb-4 size-12 text-default-200" />
      <p class="text-default-400">{{ emptyHint }}</p>
      <p class="mt-1 text-sm text-default-300">从既有 OAuth 客户端启用一个以接入开放 API</p>
    </div>

    <div v-else class="space-y-4">
      <KunCard
        v-for="app in apps"
        :key="app.client_id"
        content-class="justify-start gap-0"
        class-name="p-6"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="text-lg font-semibold text-foreground">{{ app.name }}</h3>
              <KunChip :color="DEV_TIER_COLORS[app.dev_tier] ?? 'default'" variant="flat" size="sm">
                {{ app.dev_tier }}
              </KunChip>
              <KunChip
                :color="devAppReviewColor(app.review_status)"
                variant="flat"
                size="sm"
              >
                {{ devAppReviewLabel(app.review_status) }}
              </KunChip>
              <KunChip v-if="app.archived_at" color="warning" variant="flat" size="sm">
                已归档
              </KunChip>
              <KunChip v-else-if="!app.dev_enabled" color="default" variant="flat" size="sm">
                未启用
              </KunChip>
            </div>
            <p class="mt-1 truncate font-mono text-sm text-default-400">{{ app.client_id }}</p>
          </div>
          <div class="flex shrink-0 gap-1">
            <KunButton variant="flat" size="sm" @click="openConfig(app)">
              <KunIcon name="lucide:sliders-horizontal" class="mr-1 size-4" />
              配置
            </KunButton>
            <KunButton
              color="primary"
              variant="flat"
              size="sm"
              @click="navigateTo(`/devapi/${app.client_id}`)"
            >
              <KunIcon name="lucide:key-round" class="mr-1 size-4" />
              密钥 ({{ app.key_count }})
            </KunButton>
          </div>
        </div>

        <p v-if="app.review_note" class="mt-2 text-sm text-danger">
          拒绝理由：{{ app.review_note }}
        </p>

        <div class="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div>
            <p class="text-xs text-default-400">归属用户</p>
            <p class="mt-0.5 text-sm text-foreground">{{ ownerLabel(app) }}</p>
          </div>
          <div>
            <p class="text-xs text-default-400">密钥数</p>
            <p class="mt-0.5 text-sm text-foreground">{{ app.key_count }}</p>
          </div>
          <div class="col-span-2">
            <p class="text-xs text-default-400">限额</p>
            <p class="mt-0.5 text-sm text-foreground">
              {{ overrideLabel(app) ?? `默认（${devTierLimitHint(app.dev_tier)}）` }}
            </p>
          </div>
        </div>
      </KunCard>
    </div>

    <DevapiPendingApps @reviewed="refreshAll" />

    <DevapiEnableModal
      v-model:open="showEnableModal"
      :candidates="candidates"
      @enabled="handleEnabled"
    />

    <DevapiConfigModal
      v-model:open="configOpen"
      :app="editingApp"
      @updated="handleUpdated"
    />
  </div>
</template>
