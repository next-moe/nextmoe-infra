<script setup lang="ts">
const api = useApi()

interface UserBrief {
  id: number
  name: string
  avatar: string
}
interface EditorApplication {
  id: number
  user_id: number
  status: string
  reason: string
  decline_reason: string
  reviewed_at: string | null
  created_at: string
  user: UserBrief | null
}

type AppStatus = 'pending' | 'approved' | 'declined'

const STATUS_TABS: { value: AppStatus; label: string }[] = [
  { value: 'pending', label: '待审核' },
  { value: 'approved', label: '已通过' },
  { value: 'declined', label: '已拒绝' },
]
const STATUS_LABEL: Record<string, string> = {
  pending: '待审核',
  approved: '已通过',
  declined: '已拒绝',
}
const STATUS_COLOR: Record<string, 'warning' | 'success' | 'danger'> = {
  pending: 'warning',
  approved: 'success',
  declined: 'danger',
}

const statusFilter = ref<AppStatus>('pending')
const currentPage = ref(1)
const limit = 20

const { data, status, refresh } = await useApiFetch<{
  items: EditorApplication[]
  total: number
}>('/admin/editor/applications', {
  query: computed(() => ({
    status: statusFilter.value,
    page: currentPage.value,
    limit,
  })),
})
const applications = computed(() => data.value?.items ?? [])
const total = computed(() => data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))
const isLoading = computed(() => status.value === 'pending')

const setTab = (s: AppStatus) => {
  statusFilter.value = s
  currentPage.value = 1
}

const approvingId = ref<number | null>(null)
const handleApprove = async (id: number) => {
  if (approvingId.value !== null) return
  approvingId.value = id
  try {
    const res = await api.post(`/admin/editor/applications/${id}/approve`)
    if (res.code === 0) {
      useKunMessage('已通过，已授予编辑者角色', 'success')
      refresh()
    } else {
      useKunMessage(res.message || '操作失败', 'error')
    }
  } finally {
    approvingId.value = null
  }
}

const declineOpen = ref(false)
const declineTarget = ref<number | null>(null)
const declineReason = ref('')
const declineLoading = ref(false)
const openDecline = (id: number) => {
  declineTarget.value = id
  declineReason.value = ''
  declineOpen.value = true
}
const confirmDecline = async () => {
  if (declineTarget.value === null || declineLoading.value) {
    return
  }
  declineLoading.value = true
  try {
    const res = await api.post(
      `/admin/editor/applications/${declineTarget.value}/decline`,
      { reason: declineReason.value }
    )
    if (res.code === 0) {
      useKunMessage('已拒绝', 'success')
      refresh()
      declineOpen.value = false
    } else {
      useKunMessage(res.message || '操作失败', 'error')
    }
  } finally {
    declineLoading.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <div>
      <h1 class="text-foreground text-2xl font-bold">编辑者申请</h1>
      <p class="text-default-500 mt-1 text-sm">
        审核来自编辑者平台的申请：通过即授予 editor 角色（自己的条目编辑免审直接生效，受每日上限约束，不能审核他人提案）；拒绝可填写理由，用户可在冷却期后重新申请。
      </p>
    </div>

    <div class="flex gap-2">
      <KunButton
        v-for="tab in STATUS_TABS"
        :key="tab.value"
        :color="statusFilter === tab.value ? 'primary' : 'default'"
        :variant="statusFilter === tab.value ? undefined : 'flat'"
        size="sm"
        @click="setTab(tab.value)"
      >
        {{ tab.label }}
      </KunButton>
    </div>

    <p v-if="isLoading" class="text-default-500 py-8 text-center text-sm">
      加载中...
    </p>
    <p
      v-else-if="!applications.length"
      class="text-default-500 py-8 text-center text-sm"
    >
      暂无{{ STATUS_LABEL[statusFilter] }}的申请
    </p>

    <div
      v-for="app in applications"
      v-else
      :key="app.id"
      class="border-default-200 space-y-3 rounded-lg border p-4"
    >
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-foreground font-medium">
          {{ app.user?.name || `用户 #${app.user_id}` }}
        </span>
        <KunChip
          :color="STATUS_COLOR[app.status] || 'default'"
          variant="flat"
          size="sm"
        >
          {{ STATUS_LABEL[app.status] || app.status }}
        </KunChip>
        <span class="text-default-400 ml-auto text-xs">{{ app.created_at }}</span>
      </div>

      <p class="text-foreground text-sm whitespace-pre-wrap">
        申请理由：{{ app.reason }}
      </p>
      <p
        v-if="app.status === 'declined' && app.decline_reason"
        class="text-danger-600 text-sm"
      >
        拒绝理由：{{ app.decline_reason }}
      </p>

      <div v-if="app.status === 'pending'" class="flex justify-end gap-2">
        <KunButton
          color="danger"
          variant="flat"
          size="sm"
          :disabled="approvingId !== null"
          @click="openDecline(app.id)"
        >
          拒绝
        </KunButton>
        <KunButton
          color="success"
          size="sm"
          :disabled="approvingId !== null"
          @click="handleApprove(app.id)"
        >
          <KunIcon
            v-if="approvingId === app.id"
            name="lucide:loader-circle"
            class="mr-1 size-4 animate-spin"
          />
          通过
        </KunButton>
      </div>
    </div>

    <div v-if="totalPages > 1" class="flex justify-center">
      <KunPagination
        v-model:current-page="currentPage"
        :total-page="totalPages"
      />
    </div>

    <KunModal v-model="declineOpen" aria-label="拒绝申请">
      <div class="space-y-4">
        <div>
          <h2 class="text-foreground text-xl font-bold">拒绝申请</h2>
          <p class="text-default-500 mt-1 text-sm">
            理由对申请人可见。拒绝后用户可在冷却期后重新申请。
          </p>
        </div>
        <KunTextarea
          v-model="declineReason"
          placeholder="(可选) 拒绝理由"
          :rows="3"
        />
        <div class="flex justify-end gap-2">
          <KunButton
            color="default"
            variant="flat"
            :disabled="declineLoading"
            @click="declineOpen = false"
          >
            取消
          </KunButton>
          <KunButton
            color="danger"
            :disabled="declineLoading"
            @click="confirmDecline"
          >
            <KunIcon
              v-if="declineLoading"
              name="lucide:loader-circle"
              class="mr-1 size-4 animate-spin"
            />
            确认拒绝
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
