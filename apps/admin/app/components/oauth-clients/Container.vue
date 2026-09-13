<script setup lang="ts">
import { oauthClientDeleteMessage } from '~/constants/oauth-client'

const api = useApi()

const { data: clientsData, status, refresh: refreshClients, error } =
  await useApiFetch<OAuthClient[]>('/oauth/clients')
const { data: sitesData } = await useApiFetch<Site[]>('/sites')
const clients = computed(() => clientsData.value ?? [])
const sites = computed(() => sitesData.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const search = ref('')
const siteNameById = computed(() => {
  const m = new Map<number, string>()
  for (const s of sites.value) m.set(s.id, s.name)
  return m
})
const filteredClients = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return clients.value
  return clients.value.filter((c) => {
    const site = c.site_id ? (siteNameById.value.get(c.site_id) ?? '') : ''
    return (
      c.name.toLowerCase().includes(q) ||
      c.id.toLowerCase().includes(q) ||
      site.toLowerCase().includes(q) ||
      (c.redirect_uris ?? []).some((u) => u.toLowerCase().includes(q))
    )
  })
})

const showCreateModal = ref(false)
const createdClient = ref<OAuthClientCreated | null>(null)
const secretOpen = ref(false)
const editingClient = ref<OAuthClient | null>(null)
const editOpen = ref(false)

const openEdit = (client: OAuthClient) => {
  editingClient.value = client
  editOpen.value = true
}

const handleCreated = (client: OAuthClientCreated) => {
  showCreateModal.value = false
  createdClient.value = client
  secretOpen.value = true
  refreshClients()
}

const handleUpdated = () => {
  editOpen.value = false
  refreshClients()
}

const handleDelete = async (clientId: string) => {
  const response = await api.delete(`/oauth/clients/${clientId}`)
  if (response.code === 0) {
    useKunMessage('客户端已删除', 'success')
    refreshClients()
  } else {
    useKunMessage(oauthClientDeleteMessage(response.code, response.message), 'error')
  }
}
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-bold text-foreground">OAuth 客户端</h1>
        <p class="mt-1 text-default-500">管理 OAuth 2.0 客户端应用</p>
      </div>
      <KunButton color="primary" class-name="shrink-0 self-start" @click="showCreateModal = true">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        创建客户端
      </KunButton>
    </div>

    <CommonFetchError v-if="error" @retry="refreshClients" />

    <div v-else-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon name="lucide:loader-circle" class="size-8 animate-spin text-primary" />
    </div>

    <div v-else-if="clients.length === 0" class="rounded-xl bg-content1 py-12 text-center shadow-sm">
      <KunIcon name="lucide:key" class="mx-auto mb-4 size-12 text-default-200" />
      <p class="text-default-400">暂无 OAuth 客户端配置</p>
      <p class="mt-1 text-sm text-default-300">创建客户端以启用 OAuth 认证</p>
    </div>

    <template v-else>
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <KunInput
          v-model="search"
          is-clearable
          placeholder="搜索名称 / Client ID / 站点 / 回调地址"
          class="sm:max-w-md"
        >
          <template #prefix>
            <KunIcon name="lucide:search" class="size-4 text-default-400" />
          </template>
        </KunInput>
        <p class="text-default-400 shrink-0 text-sm">
          共 {{ clients.length }} 个客户端<template v-if="search.trim()">
            · 匹配 {{ filteredClients.length }} 个</template
          >
        </p>
      </div>

      <div
        v-if="filteredClients.length === 0"
        class="rounded-xl bg-content1 py-12 text-center shadow-sm"
      >
        <KunIcon name="lucide:search-x" class="mx-auto mb-3 size-10 text-default-200" />
        <p class="text-default-400">没有匹配的客户端</p>
      </div>
      <div v-else class="space-y-2">
        <OauthClientsRow
          v-for="client in filteredClients"
          :key="client.id"
          :client="client"
          :sites="sites"
          @edit="openEdit(client)"
          @delete="handleDelete(client.id)"
        />
      </div>
    </template>

    <OauthClientsCreateModal
      v-model="showCreateModal"
      :sites="sites"
      @created="handleCreated"
    />

    <OauthClientsEditModal
      v-model:open="editOpen"
      :client="editingClient"
      @updated="handleUpdated"
    />

    <OauthClientsSecretModal v-model:open="secretOpen" :client="createdClient" />
  </div>
</template>
