<script setup lang="ts">
import { DEV_APP_REVIEW_NOTE_MAX } from '~/constants/devapi'
import type { DevApp } from '~~/shared/types/devapi'

const emit = defineEmits<{ reviewed: [] }>()

const api = useApi()

const { data, status, refresh } = await useApiFetch<DevApp[]>(
  '/admin/devapi/apps?status=pending'
)

const apps = computed(() => data.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const busyId = ref<string | null>(null)
// The app outlives the close so the panel still has a name to render while
// KunModal's leave transition plays; `declineOpen` is what drives the dialog.
const declining = ref<DevApp | null>(null)
const declineOpen = ref(false)
const declineReason = ref('')
const declineError = ref('')

const afterReview = async () => {
  await refresh()
  emit('reviewed')
}

const approve = async (app: DevApp) => {
  busyId.value = app.client_id
  try {
    const res = await api.post(`/admin/devapi/apps/${app.client_id}/approve`)
    if (res.code === 0) {
      useKunMessage(`已通过「${app.name}」`, 'success')
      await afterReview()
    } else {
      useKunMessage(res.message || '通过失败', 'error')
    }
  } finally {
    busyId.value = null
  }
}

const openDecline = (app: DevApp) => {
  declining.value = app
  declineReason.value = ''
  declineError.value = ''
  declineOpen.value = true
}

const submitDecline = async () => {
  const app = declining.value
  if (!app) return
  if (!declineReason.value.trim()) {
    declineError.value = '请填写拒绝理由（申请人会看到）'
    return
  }
  busyId.value = app.client_id
  try {
    const res = await api.post(
      `/admin/devapi/apps/${app.client_id}/decline`,
      { reason: declineReason.value.trim() }
    )
    if (res.code === 0) {
      useKunMessage('已拒绝并回执理由', 'success')
      declineOpen.value = false
      await afterReview()
    } else {
      declineError.value = res.message || '拒绝失败'
    }
  } finally {
    busyId.value = null
  }
}
</script>

<template>
  <div class="space-y-3">
    <div>
      <h2 class="text-lg font-semibold text-foreground">应用创建审核</h2>
      <p class="mt-1 text-sm text-default-500">
        「自助创建应用」为需审批时，开发者提交的新应用会先落在这里，通过后才启用并可铸造密钥。
      </p>
    </div>

    <div v-if="isLoading" class="flex items-center justify-center py-8">
      <KunIcon
        name="lucide:loader-circle"
        class="size-6 animate-spin text-primary"
      />
    </div>

    <div
      v-else-if="!apps.length"
      class="rounded-xl bg-content1 py-8 text-center text-sm text-default-400 shadow-sm"
    >
      没有待审核的应用
    </div>

    <div v-else class="space-y-3">
      <KunCard
        v-for="app in apps"
        :key="app.client_id"
        content-class="justify-start gap-0 items-stretch"
        class-name="p-5"
      >
        <div class="flex flex-wrap items-center gap-2">
          <span class="font-medium text-foreground">{{ app.name }}</span>
          <KunChip color="warning" variant="flat" size="xs">待审核</KunChip>
          <span class="text-xs text-default-400">
            用户 #{{ app.owner_user_id ?? '未指定' }} ·
            {{ formatDate(app.created_at, { isShowYear: true, isPrecise: true }) }}
          </span>
        </div>

        <p class="mt-1 truncate font-mono text-xs text-default-400">
          {{ app.client_id }}
        </p>

        <div class="mt-3 flex justify-end gap-2">
          <KunButton
            color="danger"
            variant="flat"
            size="sm"
            :disabled="busyId === app.client_id"
            @click="openDecline(app)"
          >
            拒绝
          </KunButton>
          <KunButton
            color="primary"
            size="sm"
            :disabled="busyId === app.client_id"
            @click="approve(app)"
          >
            <KunIcon
              v-if="busyId === app.client_id"
              name="lucide:loader-circle"
              class="mr-1 size-4 animate-spin"
            />
            通过
          </KunButton>
        </div>
      </KunCard>
    </div>

    <KunModal
      v-model="declineOpen"
      size="md"
      :aria-label="declining ? `拒绝「${declining.name}」` : '拒绝申请'"
    >
      <div v-if="declining" class="space-y-4">
        <h2 class="text-xl font-bold text-foreground">
          拒绝「{{ declining.name }}」
        </h2>
        <p class="text-sm text-default-500">
          理由会原样回执给申请人（用户 #{{ declining.owner_user_id ?? '未指定' }}），
          其可修改后重新提交。
        </p>
        <KunTextarea
          v-model="declineReason"
          label="拒绝理由"
          placeholder="例如：应用名称与用途不符，请说明具体使用场景。"
          :rows="4"
          :maxlength="DEV_APP_REVIEW_NOTE_MAX"
          show-char-count
        />
        <div
          v-if="declineError"
          class="rounded-lg bg-danger-50 p-3 text-sm text-danger"
        >
          {{ declineError }}
        </div>
        <div class="flex justify-end gap-3">
          <KunButton color="default" variant="flat" @click="declineOpen = false">
            取消
          </KunButton>
          <KunButton color="danger" @click="submitDecline">确认拒绝</KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
