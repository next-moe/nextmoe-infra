<script setup lang="ts">
const api = useApi()

const searchQuery = ref('') // bound to the input
const appliedSearch = ref('') // committed on submit (so we don't refetch per keystroke)
const currentPage = ref(1)
const limit = 20

const { data, status, refresh } = await useApiFetch<{
  users: User[]
  total: number
  page: number
  total_pages: number
}>('/admin/users', {
  query: computed(() => ({
    page: currentPage.value,
    limit,
    ...(appliedSearch.value ? { search: appliedSearch.value } : {}),
  })),
})
const users = computed(() => data.value?.users ?? [])
const totalPages = computed(() => data.value?.total_pages ?? 1)
const totalUsers = computed(() => data.value?.total ?? 0)
const isLoading = computed(() => status.value === 'pending')

const handleSearch = () => {
  currentPage.value = 1
  appliedSearch.value = searchQuery.value
}

const banOpen = ref(false)
const banTarget = ref<{ uuid: string; name: string } | null>(null)
const banLoading = ref(false)

const requestBan = (uuid: string) => {
  const u = users.value.find((x) => x.uuid === uuid)
  banTarget.value = { uuid, name: u?.name ?? uuid }
  banOpen.value = true
}

const confirmBan = async () => {
  if (!banTarget.value) return
  banLoading.value = true
  try {
    const response = await api.post(`/admin/users/${banTarget.value.uuid}/ban`)
    if (response.code === 0) {
      useKunMessage('用户已封禁', 'success')
      refresh()
    } else {
      useKunMessage(response.message || '封禁失败', 'error')
    }
  } finally {
    banLoading.value = false
    banOpen.value = false
  }
}

const handleUnban = async (uuid: string) => {
  const response = await api.post(`/admin/users/${uuid}/unban`)
  if (response.code === 0) {
    useKunMessage('用户已解封', 'success')
    refresh()
  } else {
    useKunMessage(response.message || '解封失败', 'error')
  }
}

const anonymizeOpen = ref(false)
const anonymizeTarget = ref<{ uuid: string; name: string } | null>(null)
const anonymizeLoading = ref(false)

const requestAnonymize = (user: { uuid: string; name: string }) => {
  anonymizeTarget.value = user
  anonymizeOpen.value = true
}

const confirmAnonymize = async () => {
  if (!anonymizeTarget.value) return
  anonymizeLoading.value = true
  try {
    const response = await api.post(
      `/admin/users/${anonymizeTarget.value.uuid}/anonymize`
    )
    if (response.code === 0) {
      useKunMessage('用户已注销并匿名化', 'success')
      refresh()
    } else {
      useKunMessage(response.message || '注销失败', 'error')
    }
  } finally {
    anonymizeLoading.value = false
    anonymizeOpen.value = false
  }
}

const handleDeleteSessions = async (uuid: string) => {
  const response = await api.delete(`/admin/users/${uuid}/sessions`)
  if (response.code === 0) {
    useKunMessage('用户会话已清除', 'success')
    refresh()
  } else {
    useKunMessage(response.message || '清除会话失败', 'error')
  }
}

const avatarUploadOpen = ref(false)
const avatarUploadTarget = ref<{ uuid: string; name: string } | null>(null)

const handleUploadAvatar = (user: { uuid: string; name: string }) => {
  avatarUploadTarget.value = user
  avatarUploadOpen.value = true
}

const onAvatarUploaded = () => {
  refresh()
}

const moemoepointOpen = ref(false)
const moemoepointTarget = ref<{ uuid: string; name: string; moemoepoint: number } | null>(null)

const handleMoemoepoint = (user: { uuid: string; name: string; moemoepoint: number }) => {
  moemoepointTarget.value = user
  moemoepointOpen.value = true
}

const roleOpen = ref(false)
const roleTarget = ref<{ uuid: string; name: string; roles: string[] } | null>(null)

const handleRoles = (user: { uuid: string; name: string; roles: string[] }) => {
  roleTarget.value = user
  roleOpen.value = true
}

const siteRoleOpen = ref(false)
const siteRoleTarget = ref<{ uuid: string; name: string } | null>(null)

const handleSiteRoles = (user: { uuid: string; name: string }) => {
  siteRoleTarget.value = user
  siteRoleOpen.value = true
}

const detailOpen = ref(false)
const detailTarget = ref<{ uuid: string; name: string } | null>(null)
const handleDetail = (user: { uuid: string; name: string }) => {
  detailTarget.value = user
  detailOpen.value = true
}

const editOpen = ref(false)
const editTarget = ref<{ uuid: string; name: string } | null>(null)
const handleEdit = (user: { uuid: string; name: string }) => {
  editTarget.value = user
  editOpen.value = true
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold text-foreground">用户管理</h1>
        <p class="mt-1 text-default-500">
          共 {{ totalUsers }} 个用户
        </p>
      </div>
    </div>

    <KunCard content-class="justify-start gap-0" class-name="p-4">
      <form class="flex gap-3" @submit.prevent="handleSearch">
        <KunInput
          v-model="searchQuery"
          type="text"
          placeholder="搜索用户名或邮箱..."
          class="flex-1"
        />
        <KunButton color="primary" type="submit" :disabled="isLoading">
          <KunIcon name="lucide:search" class="mr-1 size-4" />
          搜索
        </KunButton>
      </form>
    </KunCard>

    <div v-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="size-8 animate-spin text-primary" />
    </div>

    <template v-else>
      <UsersTable
        :users="users"
        @ban="requestBan"
        @unban="handleUnban"
        @anonymize="requestAnonymize"
        @delete-sessions="handleDeleteSessions"
        @upload-avatar="handleUploadAvatar"
        @moemoepoint="handleMoemoepoint"
        @roles="handleRoles"
        @site-roles="handleSiteRoles"
        @detail="handleDetail"
        @edit="handleEdit"
      />

      <div v-if="totalPages > 1" class="flex justify-center">
        <KunPagination
          v-model:current-page="currentPage"
          :total-page="totalPages"
          :is-loading="isLoading"
        />
      </div>
    </template>

    <UsersAvatarUploadModal
      v-model:open="avatarUploadOpen"
      :user="avatarUploadTarget"
      @success="onAvatarUploaded"
    />

    <UsersMoemoepointModal
      v-model:open="moemoepointOpen"
      :user="moemoepointTarget"
      @success="refresh"
    />

    <UsersRoleModal
      v-model:open="roleOpen"
      :user="roleTarget"
      @success="refresh"
    />

    <UsersSiteRoleModal
      v-model:open="siteRoleOpen"
      :user="siteRoleTarget"
      @success="refresh"
    />

    <UsersDetailDrawer v-model:open="detailOpen" :user="detailTarget" />

    <UsersEditModal
      v-model:open="editOpen"
      :user="editTarget"
      @success="refresh"
    />

    <KunModal v-model="banOpen" aria-label="封禁用户">
      <div class="space-y-4">
        <h2 class="text-xl font-bold text-foreground">封禁用户</h2>
        <p class="rounded-lg bg-danger-50 p-3 text-sm text-danger-700">
          将封禁
          <span class="font-semibold">{{ banTarget?.name }}</span>
          ：立即强制下线（清除其所有会话与刷新令牌），之后无法再次登录或刷新令牌。可随时「解除封禁」恢复。确认继续？
        </p>
        <div class="flex justify-end gap-3">
          <KunButton
            color="default"
            variant="flat"
            :disabled="banLoading"
            @click="banOpen = false"
          >
            取消
          </KunButton>
          <KunButton color="danger" :disabled="banLoading" @click="confirmBan">
            <KunIcon
              v-if="banLoading"
              name="lucide:loader-circle"
              class="mr-2 size-4 animate-spin"
            />
            确认封禁
          </KunButton>
        </div>
      </div>
    </KunModal>

    <KunModal v-model="anonymizeOpen" aria-label="注销并匿名化">
      <div class="space-y-4">
        <h2 class="text-xl font-bold text-foreground">注销并匿名化</h2>
        <p class="rounded-lg bg-danger-50 p-3 text-sm text-danger-700">
          将<span class="font-semibold">不可逆</span>地清除
          <span class="font-semibold">{{ anonymizeTarget?.name }}</span>
          的个人信息（用户名、邮箱、头像、简介、密码全部抹除），并永久封禁、强制下线。账号本身保留（其 ID 仍被站内内容引用，显示为「已注销用户」），但<span class="font-semibold">无法恢复</span>。
        </p>
        <p class="text-default-500 text-xs">
          提示：此操作只处理身份信息。其在 Wiki 留下的 galgame 内容需在 Wiki 管理端单独软删。
        </p>
        <div class="flex justify-end gap-3">
          <KunButton
            color="default"
            variant="flat"
            :disabled="anonymizeLoading"
            @click="anonymizeOpen = false"
          >
            取消
          </KunButton>
          <KunButton
            color="danger"
            :disabled="anonymizeLoading"
            @click="confirmAnonymize"
          >
            <KunIcon
              v-if="anonymizeLoading"
              name="lucide:loader-circle"
              class="mr-2 size-4 animate-spin"
            />
            确认注销
          </KunButton>
        </div>
      </div>
    </KunModal>
  </div>
</template>
