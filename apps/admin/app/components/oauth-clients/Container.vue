<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'
import {
  OAUTH_CLIENT_VIEWS,
  oauthClientDeleteMessage,
  type OAuthClientView
} from '~/constants/oauth-client'

interface ClientGroup {
  key: string
  title: string
  hint?: string
  to?: RouteLocationRaw
  clients: OAuthClient[]
}

const api = useApi()
const route = useRoute()

const {
  data: clientsData,
  status,
  refresh: refreshClients,
  error
} = await useApiFetch<OAuthClient[]>('/oauth/clients')
const { data: sitesData } = await useApiFetch<Site[]>('/sites')
const clients = computed(() => clientsData.value ?? [])
const sites = computed(() =>
  [...(sitesData.value ?? [])].sort((a, b) => a.id - b.id)
)
const isLoading = computed(() => status.value === 'pending')

const siteById = computed(() => new Map(sites.value.map((s) => [s.id, s])))

const view = computed<OAuthClientView>(() => {
  const site = Number(route.query.site)
  if (Number.isInteger(site) && site > 0) return `site-${site}`
  const v = route.query.view
  return typeof v === 'string' && v in OAUTH_CLIENT_VIEWS
    ? (v as OAuthClientView)
    : 'mine'
})
const viewSite = computed(() =>
  view.value.startsWith('site-')
    ? siteById.value.get(Number(view.value.slice(5)))
    : undefined
)

const inView = (c: OAuthClient, v: OAuthClientView) => {
  if (v === 'mine') return c.mine
  if (v === 'all') return true
  if (v === 'storage') return oauthClientHasStorage(c)
  if (v === 'service' || v === 'developer') return oauthClientKind(c) === v
  return c.site_id === Number(v.slice(5))
}
const countOf = (v: OAuthClientView) =>
  clients.value.filter((c) => inView(c, v)).length

const navSections = computed(() => [
  {
    title: '',
    items: (['mine', 'all'] as const).map((key) => ({
      key,
      label: OAUTH_CLIENT_VIEWS[key].label,
      icon: OAUTH_CLIENT_VIEWS[key].icon,
      count: countOf(key),
      to: { query: { view: key } }
    }))
  },
  {
    title: '站点',
    items: sites.value.map((s) => ({
      key: `site-${s.id}`,
      label: s.name,
      hint: s.domain,
      icon: 'lucide:globe',
      count: countOf(`site-${s.id}`),
      to: { query: { site: s.id } }
    }))
  },
  {
    title: '类型',
    items: (['service', 'developer', 'storage'] as const).map((key) => ({
      key,
      label: OAUTH_CLIENT_VIEWS[key].label,
      icon: OAUTH_CLIENT_VIEWS[key].icon,
      count: countOf(key),
      to: { query: { view: key } }
    }))
  }
])

const search = ref('')
const query = computed(() => search.value.trim().toLowerCase())
const siteNameOf = (c: OAuthClient) =>
  c.site_id ? (siteById.value.get(c.site_id)?.name ?? '') : ''

const visible = computed(() =>
  query.value
    ? clients.value.filter((c) =>
        oauthClientMatches(c, siteNameOf(c), query.value)
      )
    : clients.value.filter((c) => inView(c, view.value))
)

const isSingleGroup = computed(
  () =>
    !query.value &&
    (Boolean(viewSite.value) ||
      view.value === 'service' ||
      view.value === 'developer')
)

const groups = computed<ClientGroup[]>(() => {
  if (isSingleGroup.value) {
    return [{ key: view.value, title: '', clients: visible.value }]
  }
  const bySite = new Map<number, OAuthClient[]>()
  const service: OAuthClient[] = []
  const developer: OAuthClient[] = []
  for (const c of visible.value) {
    const kind = oauthClientKind(c)
    if (kind === 'service') service.push(c)
    else if (kind === 'developer') developer.push(c)
    else bySite.set(c.site_id!, [...(bySite.get(c.site_id!) ?? []), c])
  }
  const out: ClientGroup[] = []
  for (const s of sites.value) {
    const list = bySite.get(s.id)
    if (!list) continue
    out.push({
      key: `site-${s.id}`,
      title: s.name,
      hint: s.domain,
      to: { query: { site: s.id } },
      clients: list
    })
    bySite.delete(s.id)
  }
  for (const [id, list] of bySite) {
    out.push({ key: `site-${id}`, title: `站点 #${id}`, clients: list })
  }
  if (service.length) {
    out.push({
      key: 'service',
      title: OAUTH_CLIENT_VIEWS.service.label,
      to: { query: { view: 'service' } },
      clients: service
    })
  }
  if (developer.length) {
    out.push({
      key: 'developer',
      title: OAUTH_CLIENT_VIEWS.developer.label,
      to: { query: { view: 'developer' } },
      clients: developer
    })
  }
  return out
})

const heading = computed(() => {
  if (query.value) {
    return {
      title: '搜索结果',
      description: `在全部 ${clients.value.length} 个客户端中找到 ${visible.value.length} 个`
    }
  }
  if (viewSite.value) {
    return { title: viewSite.value.name, description: viewSite.value.domain }
  }
  if (view.value.startsWith('site-')) {
    return { title: `站点 #${view.value.slice(5)}`, description: '' }
  }
  const v = OAUTH_CLIENT_VIEWS[view.value as keyof typeof OAUTH_CLIENT_VIEWS]
  return { title: v.label, description: v.description }
})

const showCreateModal = ref(false)
const createdClient = ref<OAuthClientCreated | null>(null)
const secretOpen = ref(false)
const editingClient = ref<OAuthClient | null>(null)
const editOpen = ref(false)
const storageOpen = ref(false)

const openEdit = (client: OAuthClient) => {
  editingClient.value = client
  editOpen.value = true
}

const openStorage = (client: OAuthClient) => {
  editingClient.value = client
  storageOpen.value = true
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

const handleStorageUpdated = () => {
  storageOpen.value = false
  useKunMessage('存储配置已更新', 'success')
  refreshClients()
}

const handleDelete = async (client: OAuthClient) => {
  const confirmed = await useKunAlert({
    title: `删除「${client.name}」？`,
    message:
      '删除后使用这个 Client ID 的登录和接口调用会立即失效，且无法恢复。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  const response = await api.delete(`/oauth/clients/${client.id}`)
  if (response.code === 0) {
    useKunMessage('客户端已删除', 'success')
    refreshClients()
  } else {
    useKunMessage(
      oauthClientDeleteMessage(response.code, response.message),
      'error'
    )
  }
}
</script>

<template>
  <div class="space-y-6">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
    >
      <div>
        <h1 class="text-foreground text-2xl font-bold">OAuth 客户端</h1>
        <p class="text-default-500 mt-1">
          按站点查看和配置登录、存储与开放 API 能力
        </p>
      </div>
      <KunButton
        color="primary"
        class-name="shrink-0 self-start"
        @click="showCreateModal = true"
      >
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        创建客户端
      </KunButton>
    </div>

    <CommonFetchError v-if="error" @retry="refreshClients" />

    <div v-else-if="isLoading" class="flex items-center justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <div v-else class="grid gap-6 lg:grid-cols-[15rem_minmax(0,1fr)]">
      <OauthClientsViewNav
        :sections="navSections"
        :active="query ? '' : view"
      />

      <section class="min-w-0 space-y-4">
        <div
          class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"
        >
          <div class="min-w-0 flex-1">
            <h2 class="text-foreground truncate text-lg font-semibold">
              {{ heading.title }}
            </h2>
            <p v-if="heading.description" class="text-default-500 text-sm">
              {{ heading.description }}
            </p>
          </div>
          <div class="sm:w-72 sm:shrink-0">
            <KunInput
              v-model="search"
              is-clearable
              placeholder="在全部客户端中搜索名称 / ID / 回调地址"
            >
              <template #prefix>
                <KunIcon name="lucide:search" class="text-default-400 size-4" />
              </template>
            </KunInput>
          </div>
        </div>

        <div
          v-if="visible.length === 0"
          class="bg-content1 rounded-xl py-12 text-center shadow-sm"
        >
          <KunIcon
            :name="query ? 'lucide:search-x' : 'lucide:key'"
            class="text-default-200 mx-auto mb-3 size-10"
          />
          <p class="text-default-400">
            {{ query ? '没有匹配的客户端' : '这里还没有客户端' }}
          </p>
          <KunButton
            v-if="!query && view !== 'all'"
            variant="light"
            color="primary"
            size="sm"
            class-name="mt-2"
            @click="navigateTo({ query: { view: 'all' } })"
          >
            查看全部客户端
          </KunButton>
        </div>

        <div v-else class="space-y-6">
          <OauthClientsGroup
            v-for="group in groups"
            :key="group.key"
            :title="group.title"
            :hint="group.hint"
            :to="group.to"
            :count="group.clients.length"
          >
            <OauthClientsRow
              v-for="client in group.clients"
              :key="client.id"
              :client="client"
              @edit="openEdit(client)"
              @storage="openStorage(client)"
              @delete="handleDelete(client)"
            />
          </OauthClientsGroup>
        </div>
      </section>
    </div>

    <OauthClientsCreateModal
      v-model="showCreateModal"
      :sites="sites"
      :default-site-id="viewSite?.id"
      @created="handleCreated"
    />

    <OauthClientsEditModal
      v-model:open="editOpen"
      :client="editingClient"
      @updated="handleUpdated"
    />

    <OauthClientsStorageModal
      v-model:open="storageOpen"
      :client="editingClient"
      @updated="handleStorageUpdated"
    />

    <OauthClientsSecretModal
      v-model:open="secretOpen"
      :client="createdClient"
    />
  </div>
</template>
