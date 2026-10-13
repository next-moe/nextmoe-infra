<script setup lang="ts">
import { EXPLORE_SOURCE_URL } from '~/constants/explore'
import type {
  ExploreEntityTarget,
  ExploreWork,
  ExploreWorkCompany
} from '~~/shared/types/explore'

const props = defineProps<{ work: ExploreWork }>()
const emit = defineEmits<{ open: [target: ExploreEntityTarget] }>()

const ALIAS_CAP = 6

const officialTitles = computed(() =>
  (props.work.titles ?? []).filter((t) => t.title_kind === 'official' && t.lang)
)
const aliases = computed(() => [
  ...new Set(
    (props.work.titles ?? [])
      .filter((t) => t.title_kind !== 'official')
      .map((t) => t.title)
  )
])
const showAllAliases = ref(false)
const visibleAliases = computed(() =>
  showAllAliases.value ? aliases.value : aliases.value.slice(0, ALIAS_CAP)
)

const companiesBy = (roles: string[]): ExploreWorkCompany[] =>
  (props.work.companies ?? []).filter((c) =>
    roles.includes(c.attribution_role ?? '')
  )
const makers = computed(() => companiesBy(['developer', 'brand', 'circle']))
const publishers = computed(() => companiesBy(['publisher']))

const platforms = computed(() => {
  const count = new Map<string, number>()
  for (const r of props.work.releases ?? [])
    for (const p of r.platforms ?? []) count.set(p, (count.get(p) ?? 0) + 1)
  return [...count.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([p]) => vocabLabel('platform', p))
})

const playtimes = computed(() =>
  [...(props.work.playtimes ?? [])].sort(
    (a, b) => (b.vote_count ?? 0) - (a.vote_count ?? 0)
  )
)

const links = computed(() =>
  (props.work.links ?? []).map((l) => ({
    url: l.url,
    label: vocabLabel('sources', l.source),
    host: l.url.replace(/^https?:\/\/(www\.)?/, '').replace(/\/$/, '')
  }))
)

const sourceRefs = computed(() => {
  const by = new Map<string, { id: string; url: string | null }[]>()
  for (const r of props.work.refs ?? []) {
    if (r.source === 'curated') continue
    if (r.source === 'vndb' && !r.external_id.startsWith('v')) continue
    const list = by.get(r.source) ?? []
    if (list.some((x) => x.id === r.external_id)) continue
    list.push({
      id: r.external_id,
      url: EXPLORE_SOURCE_URL[r.source]?.(r.external_id) ?? null
    })
    by.set(r.source, list)
  }
  return [...by.entries()].map(([source, refs]) => ({ source, refs }))
})
</script>

<template>
  <KunCard padding="none" class-name="overflow-hidden">
    <div class="border-default-200 border-b px-4 py-3">
      <h2 class="text-foreground text-sm font-semibold">作品信息</h2>
    </div>

    <dl class="divide-default-100 divide-y text-sm">
      <div v-if="officialTitles.length" class="space-y-1.5 px-4 py-3">
        <dt class="text-default-400 text-xs">官方名称</dt>
        <dd
          v-for="t in officialTitles"
          :key="`${t.lang}-${t.title}`"
          class="flex gap-2"
        >
          <span class="text-default-400 w-16 shrink-0 text-xs leading-5">
            {{ vocabLabel('olang', t.lang) }}
          </span>
          <span class="text-foreground min-w-0 break-words">
            {{ t.title }}
            <KunChip
              v-if="t.is_machine"
              color="warning"
              variant="flat"
              size="xs"
            >
              机翻
            </KunChip>
          </span>
        </dd>
      </div>

      <div v-if="aliases.length" class="space-y-1.5 px-4 py-3">
        <dt class="text-default-400 text-xs">别名</dt>
        <dd class="flex flex-wrap gap-1">
          <KunChip
            v-for="a in visibleAliases"
            :key="a"
            color="default"
            variant="flat"
            size="xs"
          >
            {{ a }}
          </KunChip>
          <KunButton
            v-if="aliases.length > ALIAS_CAP"
            variant="light"
            color="primary"
            size="xs"
            @click="showAllAliases = !showAllAliases"
          >
            {{ showAllAliases ? '收起' : `全部 ${aliases.length} 个` }}
          </KunButton>
        </dd>
      </div>

      <ExploreWorkInfoRow v-if="makers.length" label="开发">
        <button
          v-for="c in makers"
          :key="c.id"
          type="button"
          class="text-primary block text-left hover:underline"
          @click="emit('open', { kind: 'companies', id: c.id })"
        >
          {{ c.display_name }}
        </button>
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="publishers.length" label="发行">
        <button
          v-for="c in publishers"
          :key="c.id"
          type="button"
          class="text-primary block text-left hover:underline"
          @click="emit('open', { kind: 'companies', id: c.id })"
        >
          {{ c.display_name }}
        </button>
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="(work.series ?? []).length" label="系列">
        <p v-for="s in work.series" :key="s.id">
          {{ s.display_name }}
          <span v-if="s.member_count" class="text-default-400 text-xs">
            共 {{ s.member_count }} 部
          </span>
        </p>
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="work.release_date" label="发售日">
        {{ work.release_date }}
        <span v-if="work.release_status" class="text-default-400 text-xs">
          {{ vocabLabel('release_status', work.release_status) }}
        </span>
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="work.olang" label="原语言">
        {{ vocabLabel('olang', work.olang) }}
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="platforms.length" label="平台">
        {{ platforms.join(' / ') }}
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="(work.engines ?? []).length" label="引擎">
        {{ (work.engines ?? []).map((e) => e.display_name).join(' / ') }}
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="playtimes.length" label="游玩时长">
        <p v-for="p in playtimes" :key="p.source">
          {{ formatPlaytime(p.minutes) }}
          <span class="text-default-400 text-xs">
            {{ vocabLabel('sources', p.source) }}
            <template v-if="p.vote_count"> · {{ p.vote_count }} 票</template>
          </span>
        </p>
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow v-if="work.content_rating" label="分级">
        {{ vocabLabel('content_rating', work.content_rating) }}
      </ExploreWorkInfoRow>
      <ExploreWorkInfoRow label="NextMoe ID">
        <span class="font-mono">{{ work.id }}</span>
      </ExploreWorkInfoRow>
    </dl>

    <div v-if="links.length" class="border-default-200 border-t px-4 py-3">
      <h3 class="text-default-400 mb-2 text-xs">外部链接</h3>
      <ul class="space-y-1.5 text-sm">
        <li v-for="l in links" :key="l.url" class="flex min-w-0 gap-2">
          <span class="text-default-400 w-16 shrink-0 text-xs leading-5">
            {{ l.label }}
          </span>
          <KunLink
            :href="l.url"
            target="_blank"
            size="sm"
            underline="hover"
            class-name="min-w-0 truncate"
          >
            {{ l.host }}
          </KunLink>
        </li>
      </ul>
    </div>

    <div v-if="sourceRefs.length" class="border-default-200 border-t px-4 py-3">
      <h3 class="text-default-400 mb-2 text-xs">数据来源条目</h3>
      <div class="flex flex-wrap gap-1.5">
        <template v-for="g in sourceRefs" :key="g.source">
          <template v-for="r in g.refs" :key="`${g.source}-${r.id}`">
            <KunLink
              v-if="r.url"
              :href="r.url"
              target="_blank"
              underline="none"
            >
              <KunChip color="primary" variant="flat" size="xs">
                {{ vocabLabel('sources', g.source) }} · {{ r.id }}
              </KunChip>
            </KunLink>
            <KunChip v-else color="default" variant="flat" size="xs">
              {{ vocabLabel('sources', g.source) }} · {{ r.id }}
            </KunChip>
          </template>
        </template>
      </div>
    </div>
  </KunCard>
</template>
