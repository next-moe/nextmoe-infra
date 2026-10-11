<script setup lang="ts">
useSeoMeta({ title: '作品预览', robots: 'noindex' })

interface Img {
  url: string
  sexual?: string | null
  violence?: string | null
  source?: string
  portrait_pinned?: boolean
}
interface WorkCharacter {
  id: string
  display_name: string
  roster_role?: string
  spoiler?: string
  image?: Img | null
}
interface CreditEntry {
  id: string
  display_name: string
  character_id?: string | null
}
interface CreditGroup {
  role_key: string
  role_name: string
  credits: CreditEntry[]
}
interface WorkDetail {
  id: string
  display_name?: string
  content_rating?: string
  release_date?: string | null
  olang?: string
  covers?: Img[]
  screenshots?: Img[]
  tags?: {
    display_name: string
    source?: string
    spoiler?: string
    is_sexual?: boolean
  }[]
  characters?: WorkCharacter[]
  refs?: { source: string; external_id: string }[]
  releases?: {
    id: string
    release_kind?: string
    date?: string | null
    title?: string | null
    lang?: string
    platforms?: string[]
  }[]
  ratings?: {
    source: string
    score: number
    vote_count?: number
    rank?: number | null
  }[]
  playtimes?: { source: string; minutes: number; vote_count?: number }[]
  popularity?: { source: string; metric: string; value: number }[]
  credits?: CreditGroup[]
  relations?: {
    relation_type?: string
    phrase?: string
    work?: { id: string; display_name?: string; content_rating?: string }
  }[]
  companies?: {
    id: string
    display_name: string
    company_kind?: string
    attribution_role?: string
  }[]
  series?: { id: string; display_name: string; member_count?: number }[]
  intros?: { lang: string; value: string; is_machine?: boolean; source?: string }[]
  titles?: {
    title_kind?: string
    lang?: string
    latin?: string | null
    title: string
  }[]
}

// v2 is default-thin: every block below the identity core has to be asked for
// by name, and an unknown token is a 400 rather than a silently missing block.
const WORK_INCLUDE = [
  'titles',
  'refs',
  'intros',
  'covers',
  'screenshots',
  'tags',
  'characters',
  'companies',
  'series',
  'releases',
  'ratings',
  'playtimes',
  'popularity',
  'relations',
  'credits'
].join(',')

const route = useRoute()
const workId = computed(() => String(route.params.id))
const nsfw = computed(() => route.query.nsfw === '1')

const apiKey = ref('')
const loading = ref(true)
const error = ref('')
const work = ref<WorkDetail | null>(null)
const entityTarget = ref<{
  kind: 'characters' | 'credit-names' | 'companies' | 'works'
  id: string
} | null>(null)

const { read, failureMessage } = useCatalogRead()

const load = async () => {
  loading.value = true
  error.value = ''
  work.value = null
  entityTarget.value = null
  try {
    work.value = await read<WorkDetail>(
      `v2/catalog/works/${workId.value}`,
      { include: WORK_INCLUDE, ...(nsfw.value && { nsfw: 'true' }) },
      apiKey.value
    )
  } catch (e) {
    error.value = failureMessage(e, apiKey.value)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  apiKey.value = sessionStorage.getItem('explore_api_key') ?? ''
  load()
})

watch(
  () => route.fullPath,
  () => {
    if (route.params.id) load()
  }
)

const titleMain = computed(() => work.value?.display_name ?? `#${workId.value}`)

const safeImg = (c: Img) =>
  nsfw.value ||
  ((c.sexual ?? 'safe') === 'safe' && (c.violence ?? 'tame') === 'tame')

const banner = computed(() => work.value?.covers?.find(safeImg)?.url ?? null)

const LOCALE_LABEL: Record<string, string> = {
  zh: '中文',
  'zh-Hans': '中文',
  'zh-Hant': '繁體中文',
  ja: '日本語',
  en: 'English'
}
interface IntroVariant {
  label: string
  text: string
  machine: boolean
  source?: string
}
const normalizeIntro = (s: string) => s.replace(/\\\n/g, '\n').trim()
const introVariants = computed<IntroVariant[]>(() => {
  const byText = new Map<string, IntroVariant>()
  for (const i of work.value?.intros ?? []) {
    const text = normalizeIntro(i.value)
    if (text && !byText.has(text))
      byText.set(text, {
        label: LOCALE_LABEL[i.lang] ?? i.lang,
        text,
        machine: i.is_machine === true,
        source: i.source
      })
  }
  return [...byText.values()]
})
const introIdx = ref(0)
watch(
  introVariants,
  (v) => {
    const zh = v.findIndex((x) => x.label.includes('中文'))
    introIdx.value = zh >= 0 ? zh : 0
  },
  { immediate: true }
)
const activeIntro = computed(() => introVariants.value[introIdx.value] ?? null)

const coverList = computed(() =>
  (work.value?.covers ?? []).filter(safeImg).slice(0, 8)
)

const TAG_CAP = 40
const openTags = computed(() =>
  (work.value?.tags ?? []).filter((t) => (t.spoiler ?? 'none') === 'none')
)
const tags = computed(() => openTags.value.slice(0, TAG_CAP))
const tagRest = computed(() => Math.max(0, openTags.value.length - TAG_CAP))

const shots = computed(() =>
  (work.value?.screenshots ?? []).filter(safeImg).slice(0, 8)
)

const chars = computed(() =>
  (work.value?.characters ?? [])
    .filter((c) => (c.spoiler ?? 'none') === 'none')
    .slice(0, 12)
)
const charsHidden = computed(
  () =>
    (work.value?.characters ?? []).filter(
      (c) => (c.spoiler ?? 'none') !== 'none'
    ).length
)

// v2 has no per-character voice block. A credit whose `character_id` is set IS
// the voice credit for that character, so both directions come off the credits
// block already on the page instead of one request per character.
const voicesByCharacter = computed(() => {
  const m = new Map<string, CreditEntry[]>()
  for (const g of work.value?.credits ?? [])
    for (const cr of g.credits) {
      if (!cr.character_id) continue
      const list = m.get(cr.character_id) ?? []
      list.push(cr)
      m.set(cr.character_id, list)
    }
  return m
})
const characterName = computed(() => {
  const m = new Map<string, string>()
  for (const c of work.value?.characters ?? []) m.set(c.id, c.display_name)
  return m
})

const releases = computed(() => (work.value?.releases ?? []).slice(0, 10))
const releaseRest = computed(
  () => Math.max(0, (work.value?.releases?.length ?? 0) - 10)
)

const fmt = (n: number) => n.toLocaleString()

const scorePct = (r: { score: number }) =>
  Math.max(0, Math.min(100, r.score > 10 ? r.score : r.score * 10))

const METRIC_LABEL: Record<string, string> = {
  downloads: '下载数',
  wishlist: '愿望单',
  reviews: '评论数',
  bgm_wish: '想玩',
  bgm_collect: '玩过',
  bgm_doing: '在玩',
  bgm_on_hold: '搁置',
  bgm_dropped: '抛弃'
}
const popGroups = computed(() => {
  const by = new Map<string, { metric: string; value: number }[]>()
  for (const pp of work.value?.popularity ?? []) {
    const arr = by.get(pp.source) ?? []
    arr.push({ metric: pp.metric, value: pp.value })
    by.set(pp.source, arr)
  }
  return [...by.entries()].map(([source, rows]) => ({
    source,
    max: Math.max(...rows.map((r) => r.value), 1),
    rows
  }))
})
</script>

<template>
  <div class="mx-auto w-full max-w-5xl space-y-8 px-4 py-8 md:px-6">
    <NuxtLink
      to="/explore"
      class="inline-flex items-center gap-1.5 text-sm text-default-500 transition-colors hover:text-foreground"
    >
      <KunIcon name="lucide:arrow-left" class="size-4" />
      返回数据浏览
    </NuxtLink>

    <KunCard v-if="loading" content-class="p-10">
      <p class="text-center text-default-400">正在从开放 API 拉取全量数据…</p>
    </KunCard>

    <div
      v-else-if="error"
      class="rounded-lg bg-danger-50 p-3 text-sm text-danger"
    >
      {{ error }}
    </div>

    <template v-else-if="work">
      <section
        v-if="banner"
        class="relative overflow-hidden rounded-2xl border border-default-200"
      >
        <KunImageNative
          :src="banner"
          :alt="titleMain"
          loading="eager"
          class-name="max-h-96 min-h-56 w-full object-cover"
        />
        <div
          class="absolute inset-x-0 bottom-0 flex items-end gap-4 bg-background/85 p-4 backdrop-blur md:p-5"
        >
          <div class="min-w-0">
            <h1
              class="truncate text-2xl font-bold tracking-tight text-foreground md:text-3xl"
            >
              {{ titleMain }}
            </h1>
          </div>
        </div>
      </section>

      <header class="space-y-2">
        <template v-if="!banner">
          <h1 class="text-3xl font-bold tracking-tight text-foreground">
            {{ titleMain }}
          </h1>
        </template>
        <div class="flex flex-wrap items-center gap-2 pt-1">
          <KunChip
            v-if="work.content_rating === 'r18'"
            color="danger"
            variant="flat"
            size="sm"
          >
            R18
          </KunChip>
          <KunChip
            v-if="work.release_date"
            color="default"
            variant="flat"
            size="sm"
          >
            {{ work.release_date }}
          </KunChip>
          <KunChip v-if="work.olang" color="default" variant="flat" size="sm">
            原语言 {{ work.olang }}
          </KunChip>
          <button
            v-for="l in work.companies ?? []"
            :key="`l-${l.id}`"
            type="button"
            @click="entityTarget = { kind: 'companies', id: l.id }"
          >
            <KunChip color="primary" variant="flat" size="sm">
              {{ l.display_name }}
            </KunChip>
          </button>
          <KunChip
            v-for="sr in work.series ?? []"
            :key="`s-${sr.id}`"
            color="secondary"
            variant="flat"
            size="sm"
          >
            系列 · {{ sr.display_name }}（{{ sr.member_count }} 部）
          </KunChip>
        </div>
      </header>

      <section v-if="(work.titles ?? []).length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">名称</h2>
        <div class="space-y-1 text-sm text-default-500">
          <p v-for="(t, i) in work.titles" :key="`t-${i}`">
            <KunChip color="default" variant="flat" size="xs">
              {{ t.title_kind ?? 'title' }}<template v-if="t.lang"> · {{ t.lang }}</template>
            </KunChip>
            <span class="ml-2 text-foreground">{{ t.title }}</span>
            <span v-if="t.latin" class="ml-2 text-xs text-default-400">
              {{ t.latin }}
            </span>
          </p>
        </div>
      </section>

      <section
        v-if="(work.ratings ?? []).length || (work.playtimes ?? []).length"
        class="grid grid-cols-2 gap-3 md:grid-cols-4"
      >
        <div
          v-for="r in work.ratings ?? []"
          :key="`r-${r.source}`"
          class="rounded-xl border border-default-200 bg-content1 px-4 py-3"
        >
          <p class="text-xs uppercase tracking-wide text-default-400">
            {{ r.source }}
          </p>
          <p class="mt-1 text-2xl font-bold text-foreground">{{ r.score }}</p>
          <p class="text-xs text-default-400">
            {{ fmt(r.vote_count ?? 0) }} 票<template v-if="r.rank">
              · rank {{ fmt(r.rank) }}</template
            >
          </p>
          <div
            class="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-default-200"
          >
            <div
              class="h-full rounded-full bg-primary"
              :style="{ width: `${scorePct(r)}%` }"
            />
          </div>
        </div>
        <div
          v-for="p in work.playtimes ?? []"
          :key="`p-${p.source}`"
          class="rounded-xl border border-default-200 bg-content1 px-4 py-3"
        >
          <p class="text-xs uppercase tracking-wide text-default-400">
            {{ p.source }} · 时长
          </p>
          <p class="mt-1 text-2xl font-bold text-foreground">
            {{ Math.round(p.minutes / 60) }}
            <span class="text-sm font-normal">小时</span>
          </p>
          <p class="text-xs text-default-400">{{ fmt(p.minutes) }} 分钟</p>
        </div>
      </section>

      <section v-if="popGroups.length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">热度分布</h2>
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <div
            v-for="g in popGroups"
            :key="g.source"
            class="rounded-xl border border-default-200 bg-content1 p-4"
          >
            <p class="text-xs uppercase tracking-wide text-default-400">
              {{ g.source }}
            </p>
            <div class="mt-2 space-y-1.5">
              <div
                v-for="r in g.rows"
                :key="r.metric"
                class="flex items-center gap-2"
              >
                <span class="w-14 shrink-0 text-xs text-default-500">
                  {{ METRIC_LABEL[r.metric] ?? r.metric }}
                </span>
                <div
                  class="h-2 flex-1 overflow-hidden rounded-full bg-default-200"
                >
                  <div
                    class="h-full rounded-full bg-primary"
                    :style="{ width: `${(r.value / g.max) * 100}%` }"
                  />
                </div>
                <span
                  class="w-16 shrink-0 text-right text-xs font-medium text-foreground"
                >
                  {{ fmt(r.value) }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section
        v-if="(work.refs ?? []).length"
        class="flex flex-wrap items-center gap-2"
      >
        <span class="text-xs text-default-400">外部标识</span>
        <KunChip
          v-for="rf in work.refs"
          :key="`${rf.source}-${rf.external_id}`"
          color="default"
          variant="flat"
          size="sm"
        >
          {{ rf.source }}:{{ rf.external_id }}
        </KunChip>
      </section>

      <section v-if="introVariants.length">
        <div class="mb-2 flex flex-wrap items-center gap-2">
          <h2 class="text-lg font-semibold text-foreground">简介</h2>
          <div class="flex flex-wrap gap-1">
            <KunButton
              v-for="(v, i) in introVariants"
              :key="`iv-${i}`"
              size="xs"
              :color="i === introIdx ? 'primary' : 'default'"
              variant="flat"
              @click="introIdx = i"
            >
              {{ v.label }}
              <template v-if="v.machine">· 机翻</template>
            </KunButton>
          </div>
        </div>
        <div
          v-if="activeIntro"
          class="space-y-2 rounded-xl border border-default-200 bg-content1 p-4"
        >
          <div class="flex flex-wrap gap-1.5">
            <KunChip
              v-if="activeIntro.source"
              color="default"
              variant="flat"
              size="xs"
            >
              来源 {{ activeIntro.source }}
            </KunChip>
            <KunChip
              v-if="activeIntro.machine"
              color="warning"
              variant="flat"
              size="xs"
            >
              机器翻译（API 原样标注 machine=true）
            </KunChip>
          </div>
          <p
            class="whitespace-pre-line text-sm leading-relaxed text-default-500"
          >
            {{ activeIntro.text }}
          </p>
        </div>
      </section>

      <section v-if="coverList.length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">
          封面（{{ coverList.length }}）
        </h2>
        <div class="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6">
          <div
            v-for="(cv, i) in coverList"
            :key="cv.url"
            class="relative overflow-hidden rounded-lg border border-default-200 bg-default-100 transition-colors hover:border-primary"
          >
            <KunImageNative
              :src="cv.url"
              :alt="`封面 ${i + 1}`"
              loading="lazy"
              class-name="aspect-[3/4] w-full object-cover"
            />
            <div class="absolute bottom-1 left-1 flex flex-wrap gap-1">
              <KunChip v-if="cv.source" color="default" variant="solid" size="xs">
                {{ cv.source }}
              </KunChip>
              <KunChip
                v-if="cv.portrait_pinned"
                color="primary"
                variant="solid"
                size="xs"
              >
                主图
              </KunChip>
              <KunChip
                v-if="cv.sexual && cv.sexual !== 'safe'"
                color="danger"
                variant="solid"
                size="xs"
              >
                {{ cv.sexual }}
              </KunChip>
            </div>
          </div>
        </div>
      </section>

      <section v-if="tags.length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">标签</h2>
        <div class="flex flex-wrap gap-1.5">
          <KunChip
            v-for="t in tags"
            :key="`${t.source}-${t.display_name}`"
            :color="t.is_sexual ? 'danger' : 'default'"
            variant="flat"
            size="xs"
          >
            {{ t.display_name }}
          </KunChip>
          <span v-if="tagRest" class="text-xs text-default-400">
            +{{ tagRest }}
          </span>
        </div>
      </section>

      <section v-if="shots.length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">截图</h2>
        <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
          <div
            v-for="(sh, i) in shots"
            :key="sh.url"
            class="relative overflow-hidden rounded-lg border border-default-200 bg-default-100 transition-colors hover:border-primary"
          >
            <KunImageNative
              :src="sh.url"
              :alt="`截图 ${i + 1}`"
              loading="lazy"
              class-name="aspect-video w-full object-cover"
            />
            <div class="absolute bottom-1 left-1 flex gap-1">
              <KunChip v-if="sh.source" color="default" variant="solid" size="xs">
                {{ sh.source }}
              </KunChip>
              <KunChip
                v-if="
                  (sh.sexual && sh.sexual !== 'safe') ||
                  (sh.violence && sh.violence !== 'tame')
                "
                color="danger"
                variant="solid"
                size="xs"
              >
                {{ sh.sexual ?? 'safe' }} / {{ sh.violence ?? 'tame' }}
              </KunChip>
            </div>
          </div>
        </div>
      </section>

      <section v-if="chars.length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">角色</h2>
        <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
          <div
            v-for="c in chars"
            :key="c.id"
            class="overflow-hidden rounded-xl border border-default-200 bg-content1"
          >
            <div v-if="c.image" class="aspect-[3/4] overflow-hidden bg-default-100">
              <KunImageNative
                :src="c.image.url"
                :alt="c.display_name"
                loading="lazy"
                class-name="h-full w-full object-cover"
              />
            </div>
            <div class="p-2.5">
              <div class="flex items-center gap-1.5">
                <button
                  type="button"
                  class="truncate text-sm font-medium text-foreground hover:text-primary hover:underline"
                  @click="entityTarget = { kind: 'characters', id: c.id }"
                >
                  {{ c.display_name }}
                </button>
                <KunChip
                  v-if="c.roster_role === 'main'"
                  color="primary"
                  variant="flat"
                  size="xs"
                >
                  主役
                </KunChip>
              </div>
              <p
                v-if="voicesByCharacter.get(c.id)?.length"
                class="mt-0.5 flex flex-wrap items-center gap-1 text-xs text-default-400"
              >
                CV
                <button
                  v-for="v in voicesByCharacter.get(c.id)"
                  :key="v.id"
                  type="button"
                  class="hover:text-primary hover:underline"
                  @click="entityTarget = { kind: 'credit-names', id: v.id }"
                >
                  {{ v.display_name }}
                </button>
              </p>
            </div>
          </div>
        </div>
        <p v-if="charsHidden" class="mt-2 text-xs text-default-400">
          另有 {{ charsHidden }} 位含剧透角色未显示（spoilers 参数可控）
        </p>
      </section>

      <section v-if="(work.credits ?? []).length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">制作阵容</h2>
        <div class="space-y-3">
          <div v-for="g in work.credits" :key="g.role_key">
            <p class="text-xs font-medium text-default-400">
              {{ g.role_name }}
            </p>
            <div class="mt-1 flex flex-wrap gap-1.5">
              <button
                v-for="cr in g.credits"
                :key="`${g.role_key}-${cr.id}-${cr.character_id ?? ''}`"
                type="button"
                @click="entityTarget = { kind: 'credit-names', id: cr.id }"
              >
                <KunChip color="default" variant="flat" size="sm">
                  {{ cr.display_name
                  }}<template v-if="cr.character_id && characterName.get(cr.character_id)"
                    >（{{ characterName.get(cr.character_id) }}）</template
                  >
                </KunChip>
              </button>
            </div>
          </div>
        </div>
      </section>

      <section v-if="releases.length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">发行版本</h2>
        <KunCard content-class="p-0" class-name="overflow-hidden">
          <div
            v-for="rl in releases"
            :key="rl.id"
            class="flex flex-wrap items-center gap-2 border-b border-default-100 px-4 py-2.5 text-sm last:border-b-0"
          >
            <span class="font-mono text-xs text-default-400">{{
              rl.date ?? '—'
            }}</span>
            <span class="min-w-0 flex-1 truncate text-foreground">{{
              rl.title
            }}</span>
            <KunChip
              v-if="rl.release_kind && rl.release_kind !== 'default'"
              color="warning"
              variant="flat"
              size="xs"
            >
              {{ rl.release_kind }}
            </KunChip>
            <KunChip v-if="rl.lang" color="default" variant="flat" size="xs">
              {{ rl.lang }}
            </KunChip>
            <span class="text-xs text-default-400">
              {{ (rl.platforms ?? []).join(' / ') }}
            </span>
          </div>
        </KunCard>
        <p v-if="releaseRest" class="mt-2 text-xs text-default-400">
          另有 {{ releaseRest }} 个版本未列出
        </p>
      </section>

      <section v-if="(work.relations ?? []).length">
        <h2 class="mb-2 text-lg font-semibold text-foreground">关联作品</h2>
        <div class="grid grid-cols-1 gap-2 md:grid-cols-2">
          <NuxtLink
            v-for="(rel, i) in work.relations"
            :key="`rel-${i}`"
            :to="`/explore/work/${rel.work?.id}${nsfw ? '?nsfw=1' : ''}`"
            class="group flex items-center gap-3 rounded-xl border border-default-200 bg-content1 px-4 py-3 transition-colors hover:border-primary"
          >
            <KunChip color="secondary" variant="flat" size="xs">
              {{ rel.phrase ?? rel.relation_type }}
            </KunChip>
            <span
              class="min-w-0 flex-1 truncate text-sm font-medium text-foreground"
            >
              {{ rel.work?.display_name ?? `#${rel.work?.id}` }}
            </span>
            <KunChip
              v-if="rel.work?.content_rating === 'r18'"
              color="danger"
              variant="flat"
              size="xs"
            >
              R18
            </KunChip>
            <KunIcon
              name="lucide:arrow-right"
              class="size-4 shrink-0 text-default-400 transition-transform group-hover:translate-x-0.5"
            />
          </NuxtLink>
        </div>
      </section>

      <p class="border-t border-default-200 pt-4 text-xs text-default-400">
        本页由 NextMoe 公开 API v2 实时渲染，只用一次
        <code class="font-mono">GET /v2/catalog/works/{id}</code>
        —— 封面、标签、评分、时长、热度、角色、制作人员、关联作品全部经
        <code class="font-mono">include=</code>
        一并取回，逐字段附来源。同样的数据，你的应用也拿得到 ——
        <NuxtLink to="/docs" class="text-primary hover:underline">
          API 文档
        </NuxtLink>
        ·
        <NuxtLink to="/docs/example" class="text-primary hover:underline">
          实战示例
        </NuxtLink>
        。
      </p>
    </template>

    <ExploreEntityModal
      v-model="entityTarget"
      :api-key="apiKey"
      :nsfw="nsfw"
    />
  </div>
</template>
