<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { AGENT_API, AGENT_DEFAULT_SITE } from '~/constants/agent'
import type {
  AgentPersona,
  AgentPersonaList,
  AgentSuggestionGroup
} from '~~/shared/types/agent'
import type { Site } from '~~/shared/types/site'

const api = useApi('agent')
const oauthApi = useApi()

const sites = ref<Site[]>([])
const personas = ref<AgentPersona[]>([])
const builtin = ref<AgentSuggestionGroup[]>([])
const loading = ref(true)
const failed = ref('')
const busy = ref('')

const loadSites = async () => {
  const res = await oauthApi.get<Site[]>('/sites')
  if (res.code === 0) sites.value = res.data ?? []
}

const load = async () => {
  failed.value = ''
  const res = await api.get<AgentPersonaList>(`${AGENT_API}/personas`)
  loading.value = false
  if (res.code !== 0) {
    failed.value = res.message || '加载失败'
    return
  }
  personas.value = res.data.personas
  builtin.value = res.data.builtin_suggestions
}

onMounted(() => {
  loadSites()
  load()
})

const siteName = (id: number) => {
  if (id === AGENT_DEFAULT_SITE) return '默认（所有没有自己人设的站点）'
  const s = sites.value.find((x) => x.id === id)
  return s ? `${s.name}（${s.domain}）` : `站点 #${id}`
}

const groups = computed(() => {
  const map = new Map<number, AgentPersona[]>()
  for (const p of personas.value) {
    map.set(p.site_id, [...(map.get(p.site_id) ?? []), p])
  }
  return [...map.entries()]
    .sort(([a], [b]) => a - b)
    .map(([siteId, list]) => ({ siteId, list }))
})

const sitesWithout = computed(() =>
  sites.value.filter(
    (s) => !personas.value.some((p) => p.site_id === s.id && p.active)
  )
)

const modalOpen = ref(false)
const editing = ref<AgentPersona | null>(null)
const newSite = ref(AGENT_DEFAULT_SITE)
const openModal = (p: AgentPersona | null, siteId = AGENT_DEFAULT_SITE) => {
  editing.value = p
  newSite.value = siteId
  modalOpen.value = true
}

const activate = async (p: AgentPersona, active: boolean) => {
  busy.value = `${p.id}:active`
  try {
    const res = await api.patch<AgentPersona>(`${AGENT_API}/personas/${p.id}`, {
      active
    })
    if (res.code !== 0) {
      useKunMessage(res.message || '更新失败', 'error')
      return
    }
    useKunMessage(active ? '已启用' : '已停用', 'success')
    await load()
  } finally {
    busy.value = ''
  }
}

const remove = async (p: AgentPersona) => {
  const confirmed = await useKunAlert({
    title: `删除人设「${p.name}」?`,
    message: p.active ? '该站点之后会改用默认人设。' : '删除后无法恢复。',
    type: 'danger',
    confirmText: '删除',
    confirmColor: 'danger'
  })
  if (!confirmed) return
  busy.value = `${p.id}:delete`
  try {
    const res = await api.delete(`${AGENT_API}/personas/${p.id}`)
    if (res.code !== 0) {
      useKunMessage(res.message || '删除失败', 'error')
      return
    }
    useKunMessage('已删除', 'success')
    personas.value = personas.value.filter((x) => x.id !== p.id)
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 class="text-foreground text-2xl font-bold">AI 助手 · 人设</h1>
        <p class="text-default-500 mt-1 text-sm">
          各站看板娘的名字、头像、人设、开场白和建议问题。站点没有自己的人设时用默认人设；修改后下一问立即生效。
        </p>
      </div>
      <KunButton color="primary" @click="openModal(null)">
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建人设
      </KunButton>
    </div>

    <CommonFetchError v-if="failed" :message="failed" @retry="load" />

    <div v-if="loading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <template v-else>
      <div v-for="g in groups" :key="g.siteId" class="space-y-2">
        <h3 class="text-foreground font-semibold">{{ siteName(g.siteId) }}</h3>
        <div class="grid gap-3 lg:grid-cols-2">
          <KunCard
            v-for="p in g.list"
            :key="p.id"
            :class-name="cn('space-y-3', !p.active && 'opacity-70')"
          >
            <div class="flex items-start gap-3">
              <KunAvatar
                :user="{ id: 0, name: p.name, avatar: p.avatar }"
                size="lg"
                :is-navigation="false"
              />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <p class="text-foreground font-semibold">{{ p.name }}</p>
                  <KunChip
                    :color="p.active ? 'success' : 'default'"
                    variant="flat"
                    size="xs"
                  >
                    {{ p.active ? '生效中' : '未启用' }}
                  </KunChip>
                </div>
                <p
                  class="text-default-500 mt-1 line-clamp-3 text-sm whitespace-pre-line"
                >
                  {{ p.prompt }}
                </p>
              </div>
              <KunSwitch
                :model-value="p.active"
                :disabled="busy === `${p.id}:active`"
                :aria-label="`启用 ${p.name}`"
                @update:model-value="activate(p, $event)"
              />
            </div>
            <div
              class="text-default-500 flex flex-wrap gap-x-4 gap-y-1 text-xs"
            >
              <span>开场白 {{ p.greetings.length }} 条</span>
              <span>
                建议问题：{{
                  p.suggestions.length
                    ? `自定义 ${p.suggestions.length} 组`
                    : `内置 ${builtin.length} 组`
                }}
              </span>
            </div>
            <div class="flex justify-end gap-2">
              <KunButton size="sm" variant="light" @click="openModal(p)">
                编辑
              </KunButton>
              <KunButton
                size="sm"
                variant="light"
                color="danger"
                :loading="busy === `${p.id}:delete`"
                @click="remove(p)"
              >
                删除
              </KunButton>
            </div>
          </KunCard>
        </div>
      </div>

      <div v-if="sitesWithout.length" class="space-y-2">
        <h3 class="text-foreground font-semibold">使用默认人设的站点</h3>
        <div class="flex flex-wrap gap-2">
          <KunButton
            v-for="s in sitesWithout"
            :key="s.id"
            size="sm"
            variant="flat"
            @click="openModal(null, s.id)"
          >
            <KunIcon name="lucide:plus" class="mr-1 size-4" />
            为 {{ s.name }} 单独设置
          </KunButton>
        </div>
      </div>
    </template>

    <AiAgentPersonasModal
      v-model="modalOpen"
      :persona="editing"
      :sites="sites"
      :builtin="builtin"
      :default-site-id="newSite"
      @saved="load"
    />
  </div>
</template>
