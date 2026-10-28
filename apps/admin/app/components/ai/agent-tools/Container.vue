<script setup lang="ts">
import { AGENT_API, AGENT_TOOL_AUTH_META } from '~/constants/agent'
import type { AgentSiteTool, AgentSiteToolProbe } from '~~/shared/types/agent'
import type { Site } from '~~/shared/types/site'

const api = useApi('agent')
const oauthApi = useApi()

const sites = ref<Site[]>([])
const tools = ref<AgentSiteTool[]>([])
const siteFilter = ref<number | 'all'>('all')
const loading = ref(true)
const failed = ref('')
const busy = ref('')
const probes = ref<Record<number, AgentSiteToolProbe>>({})

const loadSites = async () => {
  const res = await oauthApi.get<Site[]>('/sites')
  if (res.code === 0) sites.value = res.data ?? []
}

const loadTools = async () => {
  failed.value = ''
  const res = await api.get<AgentSiteTool[] | null>(
    `${AGENT_API}/site-tools`,
    siteFilter.value === 'all' ? undefined : { site_id: siteFilter.value }
  )
  loading.value = false
  if (res.code !== 0) {
    failed.value = res.message || '加载失败'
    return
  }
  tools.value = res.data ?? []
}

onMounted(() => {
  loadSites()
  loadTools()
})
watch(siteFilter, loadTools)

const siteName = (id: number) => {
  const s = sites.value.find((x) => x.id === id)
  return s ? `${s.name}（${s.domain}）` : `站点 #${id}`
}

const siteOptions = computed(() => [
  { value: 'all' as const, label: '全部站点' },
  ...sites.value.map((s) => ({
    value: s.id,
    label: `${s.name}（${s.domain}）`
  }))
])

const groups = computed(() => {
  const map = new Map<number, AgentSiteTool[]>()
  for (const t of tools.value) {
    map.set(t.site_id, [...(map.get(t.site_id) ?? []), t])
  }
  return [...map.entries()]
    .sort(([a], [b]) => a - b)
    .map(([siteId, list]) => ({ siteId, list }))
})

const modalOpen = ref(false)
const editing = ref<AgentSiteTool | null>(null)
const openModal = (t: AgentSiteTool | null) => {
  editing.value = t
  modalOpen.value = true
}

const replace = (next: AgentSiteTool) => {
  tools.value = tools.value.map((t) => (t.id === next.id ? next : t))
}

const toggle = async (t: AgentSiteTool, enabled: boolean) => {
  busy.value = `${t.id}:enabled`
  try {
    const res = await api.patch<AgentSiteTool>(
      `${AGENT_API}/site-tools/${t.id}`,
      { enabled }
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '更新失败', 'error')
      return
    }
    replace(res.data)
    useKunMessage(enabled ? '已启用' : '已停用', 'success')
  } finally {
    busy.value = ''
  }
}

const test = async (t: AgentSiteTool) => {
  busy.value = `${t.id}:test`
  try {
    const res = await api.post<AgentSiteToolProbe>(
      `${AGENT_API}/site-tools/${t.id}/test`
    )
    if (res.code !== 0) {
      useKunMessage(res.message || '测试请求失败', 'error')
      return
    }
    probes.value = { ...probes.value, [t.id]: res.data }
  } finally {
    busy.value = ''
  }
}

const closeProbe = (id: number) => {
  probes.value = Object.fromEntries(
    Object.entries(probes.value).filter(([k]) => Number(k) !== id)
  )
}

const remove = async (t: AgentSiteTool) => {
  const confirmed = await useKunAlert({
    title: `删除站点工具「${t.name}」?`,
    message: 'AI 助手将不再调用这个 MCP 服务器上的工具。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = `${t.id}:delete`
  try {
    const res = await api.delete(`${AGENT_API}/site-tools/${t.id}`)
    if (res.code !== 0) {
      useKunMessage(res.message || '删除失败', 'error')
      return
    }
    useKunMessage('已删除', 'success')
    closeProbe(t.id)
    tools.value = tools.value.filter((x) => x.id !== t.id)
  } finally {
    busy.value = ''
  }
}

const onSaved = (t: AgentSiteTool) => {
  if (tools.value.some((x) => x.id === t.id)) replace(t)
  else loadTools()
}
</script>

<template>
  <div class="space-y-5">
    <div>
      <h1 class="text-foreground text-2xl font-bold">AI 助手 · 站点工具</h1>
      <p class="text-default-500 mt-1 text-sm">
        各站点通过 MCP 服务器把自己的搜索、详情等能力提供给 AI
        助手;这里登记服务器地址、鉴权方式和允许调用的工具。
      </p>
    </div>

    <div class="flex flex-wrap items-end justify-between gap-3">
      <KunSelect
        v-model="siteFilter"
        :options="siteOptions"
        aria-label="按站点筛选"
        class-name="w-full sm:w-72"
      />
      <KunButton color="primary" @click="openModal(null)">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        登记站点工具
      </KunButton>
    </div>

    <CommonFetchError v-if="failed" :message="failed" @retry="loadTools" />

    <div v-if="loading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <KunCard
      v-else-if="!groups.length && !failed"
      class-name="py-12 text-center"
    >
      <p class="text-default-400">还没有登记站点工具</p>
    </KunCard>

    <div v-for="g in groups" :key="g.siteId" class="space-y-2">
      <h3 class="text-foreground font-semibold">{{ siteName(g.siteId) }}</h3>
      <div class="bg-content1 overflow-x-auto rounded-xl shadow-sm">
        <table class="w-full min-w-[64rem] text-sm">
          <thead class="bg-content2 text-default-500">
            <tr>
              <th class="px-3 py-2 text-left font-medium">名称</th>
              <th class="px-3 py-2 text-left font-medium">MCP 地址</th>
              <th class="px-3 py-2 text-left font-medium">鉴权</th>
              <th class="px-3 py-2 text-left font-medium">卡片前缀</th>
              <th class="px-3 py-2 text-left font-medium">允许的工具</th>
              <th class="px-3 py-2 text-right font-medium">超时</th>
              <th class="px-3 py-2 text-left font-medium">启用</th>
              <th class="px-3 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="t in g.list"
              :key="t.id"
              class="border-default-200 border-t align-top"
            >
              <td class="px-3 py-2">
                <p class="text-foreground font-mono font-medium">
                  {{ t.name }}
                </p>
                <p class="text-default-400 font-mono text-xs">
                  {{ t.name }}__*
                </p>
              </td>
              <td
                class="text-default-500 max-w-72 px-3 py-2 font-mono text-xs break-all"
              >
                {{ t.url }}
              </td>
              <td class="px-3 py-2">
                <KunChip
                  :color="AGENT_TOOL_AUTH_META[t.auth].color"
                  variant="flat"
                  size="xs"
                >
                  {{ AGENT_TOOL_AUTH_META[t.auth].label }}
                </KunChip>
              </td>
              <td class="px-3 py-2 font-mono text-xs">{{ t.card_prefix }}:*</td>
              <td class="px-3 py-2">
                <div v-if="t.allowed?.length" class="flex flex-wrap gap-1">
                  <KunChip
                    v-for="a in t.allowed"
                    :key="a"
                    color="default"
                    variant="flat"
                    size="xs"
                  >
                    {{ a }}
                  </KunChip>
                </div>
                <span v-else class="text-default-500 text-xs">
                  全部(含以后新增)
                </span>
              </td>
              <td class="px-3 py-2 text-right text-xs tabular-nums">
                {{ (t.timeout_ms / 1000).toLocaleString() }} 秒
              </td>
              <td class="px-3 py-2">
                <KunSwitch
                  :model-value="t.enabled"
                  :disabled="busy === `${t.id}:enabled`"
                  @update:model-value="toggle(t, $event)"
                />
              </td>
              <td class="px-3 py-2 text-right whitespace-nowrap">
                <KunButton
                  size="sm"
                  variant="flat"
                  :loading="busy === `${t.id}:test`"
                  @click="test(t)"
                >
                  测试
                </KunButton>
                <KunButton size="sm" variant="light" @click="openModal(t)">
                  编辑
                </KunButton>
                <KunButton
                  size="sm"
                  variant="light"
                  color="danger"
                  :loading="busy === `${t.id}:delete`"
                  @click="remove(t)"
                >
                  删除
                </KunButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <template v-for="t in g.list" :key="`probe-${t.id}`">
        <AiAgentToolsProbe
          v-if="probes[t.id]"
          :tool="t"
          :probe="probes[t.id]!"
          @saved="replace"
          @close="closeProbe(t.id)"
        />
      </template>
    </div>

    <AiAgentToolsModal
      v-model="modalOpen"
      :tool="editing"
      :sites="sites"
      :default-site-id="siteFilter === 'all' ? 0 : siteFilter"
      @saved="onSaved"
    />
  </div>
</template>
