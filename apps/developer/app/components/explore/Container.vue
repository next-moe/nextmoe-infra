<script setup lang="ts">
import type { ExploreWorkBrief } from '~~/shared/types/explore'

useSeoMeta({ title: '数据浏览', robots: 'noindex' })

interface SearchHit {
  id: string | number
  object?: string
  target_object?: string
  entity_type?: string
  name?: string
  display_name?: string
  latin?: string | null
  content_rating?: string
  sources?: string[]
}

const apiKey = ref('')
const q = ref('')
const nsfw = ref(false)
const searching = ref(false)
const loadingDetail = ref(false)
const error = ref('')
const hits = ref<SearchHit[]>([])
const detail = ref<Record<string, unknown> | null>(null)
const showRaw = ref(false)

interface ExpandedEntity {
  group: string
  id: string
  name: string
  raw: Record<string, unknown>
  showRaw: boolean
}
const expanded = ref<ExpandedEntity[]>([])
const expandNote = ref('')
const expanding = ref(false)
const expandCap = computed(() => (apiKey.value.trim() ? 6 : 3))

const GROUP_KIND: Record<string, 'characters' | 'credit-names' | 'companies' | 'works'> = {
  '厂牌 / 社团': 'companies',
  '角色': 'characters',
  '名义': 'credit-names',
  '关联作品': 'works'
}
const entityTarget = ref<{
  kind: 'characters' | 'credit-names' | 'companies' | 'works'
  id: string
} | null>(null)
const openEntity = (e: ExpandedEntity) => {
  const kind = GROUP_KIND[e.group]
  if (kind) entityTarget.value = { kind, id: e.id }
}

const idOf = (v: unknown): string | null => {
  if (v && typeof v === 'object' && 'id' in v) {
    const n = (v as { id?: unknown }).id
    if (typeof n === 'string' && n) return n
    if (typeof n === 'number' && n > 0) return String(n)
  }
  return null
}

const expandAll = async () => {
  if (expanding.value || !detail.value) return
  expanding.value = true
  expandNote.value = ''
  expanded.value = []
  const d = detail.value
  const arr = (k: string): unknown[] => (Array.isArray(d[k]) ? (d[k] as unknown[]) : [])

  const jobs: {
    group: string
    id: string
    path: string
    query: Record<string, string>
  }[] = []
  const seen = new Set<string>()
  const gate = (): Record<string, string> =>
    nsfw.value ? { nsfw: 'true' } : {}
  const push = (
    group: string,
    id: string | null,
    path: (i: string) => string,
    query: Record<string, string> = {}
  ) => {
    if (id === null || seen.has(`${group}:${id}`)) return
    seen.add(`${group}:${id}`)
    jobs.push({ group, id, path: path(id), query })
  }

  for (const l of arr('companies'))
    push('厂牌 / 社团', idOf(l), (i) => `v2/catalog/companies/${i}`, {
      ...gate()
    })
  for (const c of arr('characters'))
    push('角色', idOf(c), (i) => `v2/catalog/characters/${i}`, {
      view: 'full',
      ...gate()
    })
  for (const r of arr('credits')) {
    const inner = (r as { credits?: unknown[] }).credits
    if (Array.isArray(inner))
      for (const c of inner)
        push(
          '名义',
          idOf((c as { name?: unknown }).name) ?? idOf(c),
          (i) => `v2/catalog/credit-names/${i}`,
          { ...gate() }
        )
  }
  let offCatalog = 0
  for (const r of arr('relations')) {
    const work = (r as { work?: ExploreWorkBrief }).work
    if (!isBrowsableWork(work)) {
      offCatalog++
      continue
    }
    push(
      '关联作品',
      idOf(work),
      (i) => `v2/catalog/works/${i}`,
      { include: 'relations,credits', ...gate() }
    )
  }
  const totals = new Map<string, number>()
  for (const j of jobs) totals.set(j.group, (totals.get(j.group) ?? 0) + 1)
  const taken = new Map<string, number>()
  const capped: typeof jobs = []
  for (const j of jobs) {
    const n = taken.get(j.group) ?? 0
    if (n < expandCap.value) {
      taken.set(j.group, n + 1)
      capped.push(j)
    }
  }

  const results = await Promise.allSettled(
    capped.map(async (j) => {
      const resp = await apiGet(j.path, j.query)
      return { j, data: resp as Record<string, unknown> }
    })
  )
  let failed = 0
  for (const r of results) {
    if (r.status !== 'fulfilled' || !r.value.data) {
      failed++
      continue
    }
    const { j, data } = r.value
    expanded.value.push({
      group: j.group,
      id: j.id,
      name: entityName(data) ?? `#${j.id}`,
      raw: data,
      showRaw: false
    })
  }
  const notes: string[] = []
  for (const [g, total] of totals) {
    const took = taken.get(g) ?? 0
    if (total > took) notes.push(`${g} 仅取前 ${took}/${total}`)
  }
  if (offCatalog)
    notes.push(`${offCatalog} 部关联作品不是 galgame，开放 API 不提供其详情`)
  if (failed) notes.push(`${failed} 个实体拉取失败`)
  expandNote.value = notes.join('；')
  expanding.value = false
}

const expandedGroups = computed(() => {
  const m = new Map<string, ExpandedEntity[]>()
  for (const e of expanded.value) {
    const list = m.get(e.group) ?? []
    list.push(e)
    m.set(e.group, list)
  }
  return [...m.entries()]
})

const facetsOf = (obj: Record<string, unknown>) =>
  Object.entries(obj)
    .filter(([, v]) => (Array.isArray(v) ? v.length > 0 : v !== null && v !== ''))
    .map(([k, v]) => ({ key: k, count: Array.isArray(v) ? `${v.length}` : '' }))
    .slice(0, 12)

onMounted(() => {
  apiKey.value = sessionStorage.getItem('explore_api_key') ?? ''
})
watch(apiKey, (v) => sessionStorage.setItem('explore_api_key', v))

const { read, failureMessage } = useCatalogRead()
const apiGet = (path: string, query: Record<string, string>) =>
  read<unknown>(path, query, apiKey.value)
const problemMessage = (e: unknown) => failureMessage(e, apiKey.value)

const search = async () => {
  if (searching.value) return
  searching.value = true
  error.value = ''
  detail.value = null
  try {
    const resp = await apiGet('v2/catalog/search', {
      object: 'work',
      q: q.value,
      ...(nsfw.value && { nsfw: 'true' })
    })
    hits.value = (resp as { items?: SearchHit[] }).items ?? []
  } catch (e) {
    hits.value = []
    error.value = problemMessage(e)
  } finally {
    searching.value = false
  }
}

const openDetail = async (id: string | number) => {
  if (loadingDetail.value) return
  loadingDetail.value = true
  error.value = ''
  try {
    const resp = await apiGet(`v2/catalog/works/${id}`, {
      include: 'relations,credits,companies,characters',
      ...(nsfw.value && { nsfw: 'true' })
    })
    detail.value = (resp as Record<string, unknown>) ?? null
    expanded.value = []
    expandNote.value = ''
    showRaw.value = false
  } catch (e) {
    error.value = problemMessage(e)
  } finally {
    loadingDetail.value = false
  }
}

const facetSummary = computed(() => {
  if (!detail.value) return []
  return Object.entries(detail.value)
    .map(([k, v]) => ({
      key: k,
      kind: Array.isArray(v) ? `${v.length} 条` : v === null ? 'null' : typeof v
    }))
    .sort((a, b) => a.key.localeCompare(b.key))
})

</script>

<template>
  <div class="mx-auto w-full max-w-5xl space-y-6 px-4 py-10 md:px-6">
    <div>
      <h1 class="text-2xl font-bold text-foreground">数据浏览</h1>
      <p class="mt-1 text-sm text-default-500">
        不需要 key：请求从你的浏览器直接发到
        <code class="font-mono text-xs">api.nextmoe.dev</code>，按你的 IP
        计入匿名额度（每分钟 30 次、每天 1,000 次）。想用自己的额度就填一把 key，它只存在本浏览器的
        sessionStorage；还没有的话，去
        <NuxtLink to="/dashboard" class="text-primary hover:underline">控制台</NuxtLink>
        自助铸造。
      </p>
    </div>

    <KunCard content-class="gap-4">
      <KunInput
        v-model="apiKey"
        label="API Key（可选）"
        type="password"
        placeholder="不填即匿名调用"
      />
      <div class="flex flex-wrap items-end gap-3">
        <div class="min-w-0 flex-1">
          <KunInput
            v-model="q"
            label="作品标题搜索"
            placeholder="如 SEQUEL / いろセカ"
            @keyup.enter="search"
          />
        </div>
        <div class="pb-2">
          <KunCheckBox v-model="nsfw" label="含 R18" />
        </div>
        <KunButton color="primary" :disabled="searching" @click="search">
          {{ searching ? '搜索中…' : '搜索' }}
        </KunButton>
      </div>
    </KunCard>

    <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
      {{ error }}
    </div>

    <KunCard v-if="hits.length" content-class="p-0" class-name="overflow-hidden">
      <div class="border-b border-default-200 px-4 py-2 text-xs text-default-400">
        前 {{ hits.length }} 条结果
      </div>
      <button
        v-for="h in hits"
        :key="h.id"
        type="button"
        class="flex w-full items-center gap-3 border-b border-default-100 px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-default-100"
        @click="openDetail(h.id)"
      >
        <span class="min-w-0 flex-1 truncate text-sm font-medium text-foreground">
          {{ h.display_name || h.name || `#${h.id}` }}
        </span>
        <KunChip v-if="h.content_rating === 'r18'" color="danger" variant="flat" size="xs">
          R18
        </KunChip>
        <KunChip color="default" variant="flat" size="xs">#{{ h.id }}</KunChip>
      </button>
    </KunCard>

    <KunCard v-if="loadingDetail" content-class="p-10">
      <p class="text-center text-default-400">加载详情…</p>
    </KunCard>

    <template v-if="detail && !loadingDetail">
      <KunCard content-class="gap-3">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="text-lg font-semibold text-foreground">
            {{ (detail as { display_name?: string }).display_name ?? `#${(detail as { id?: number }).id}` }}
          </h2>
          <KunChip
            v-if="(detail as { content_rating?: string }).content_rating === 'r18'"
            color="danger"
            variant="flat"
            size="xs"
          >
            R18
          </KunChip>
          <KunButton
            size="sm"
            color="primary"
            variant="flat"
            @click="
              navigateTo(
                `/explore/work/${(detail as { id?: number }).id}${nsfw ? '?nsfw=1' : ''}`
              )
            "
          >
            预览详情页
          </KunButton>
        </div>
        <div class="grid grid-cols-2 gap-2 md:grid-cols-4">
          <div
            v-for="f in facetSummary"
            :key="f.key"
            class="rounded-lg border border-default-200 px-3 py-2"
          >
            <p class="truncate font-mono text-xs text-default-400">{{ f.key }}</p>
            <p class="text-sm font-medium text-foreground">{{ f.kind }}</p>
          </div>
        </div>
        <div class="rounded-lg border border-default-200">
          <KunButton
            variant="light"
            color="default"
            size="sm"
            @click="showRaw = !showRaw"
          >
            {{ showRaw ? '收起数据树' : '展开数据树' }}
          </KunButton>
          <div v-show="showRaw" class="max-h-96 overflow-auto p-4">
            <ExploreJsonTree :data="detail" />
          </div>
        </div>

        <div class="flex flex-wrap items-center gap-3">
          <KunButton
            color="primary"
            variant="flat"
            :disabled="expanding"
            @click="expandAll"
          >
            {{ expanding ? '展开中…' : '展开实体全景' }}
          </KunButton>
          <span class="text-xs text-default-400">
            对每个关联实体（厂牌 / 角色 / 名义 / 关联作品）各打一次它自己的读面
            —— 实体为中心，每类实体一套一致的完整档案。
          </span>
        </div>
        <p v-if="expandNote" class="text-xs text-warning">{{ expandNote }}</p>

        <div
          v-for="[group, ents] in expandedGroups"
          :key="group"
          class="space-y-2"
        >
          <h3 class="text-sm font-semibold text-foreground">
            {{ group }}（{{ ents.length }}）
          </h3>
          <div class="grid grid-cols-1 gap-2 md:grid-cols-2">
            <div
              v-for="e in ents"
              :key="`${group}-${e.id}`"
              class="rounded-lg border border-default-200 px-3 py-2"
            >
              <div class="flex items-center justify-between gap-2">
                <button
                  type="button"
                  class="truncate text-sm font-medium text-foreground hover:text-primary hover:underline"
                  @click="openEntity(e)"
                >
                  {{ e.name }}
                </button>
                <KunChip color="default" variant="flat" size="xs">
                  #{{ e.id }}
                </KunChip>
              </div>
              <div class="mt-1.5 flex flex-wrap gap-1">
                <KunChip
                  v-for="f in facetsOf(e.raw)"
                  :key="f.key"
                  color="default"
                  variant="flat"
                  size="xs"
                >
                  {{ f.key }}{{ f.count ? ` ${f.count}` : '' }}
                </KunChip>
              </div>
              <KunButton
                variant="light"
                color="default"
                size="xs"
                class="mt-1.5"
                @click="e.showRaw = !e.showRaw"
              >
                {{ e.showRaw ? '收起数据树' : '数据树' }}
              </KunButton>
              <div v-if="e.showRaw" class="mt-1 max-h-64 overflow-auto rounded bg-default-100 p-2">
                <ExploreJsonTree :data="e.raw" />
              </div>
            </div>
          </div>
        </div>
      </KunCard>
    </template>

    <ExploreEntityModal
      v-model="entityTarget"
      :api-key="apiKey"
      :nsfw="nsfw"
    />
  </div>
</template>
