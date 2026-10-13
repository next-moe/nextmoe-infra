<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import type {
  ExploreEntityTarget,
  ExploreTag,
  ExploreWork
} from '~~/shared/types/explore'

const props = defineProps<{ work: ExploreWork; nsfw: boolean }>()
const emit = defineEmits<{
  open: [target: ExploreEntityTarget]
  go: [tab: string]
}>()

const TAG_CAP = 30
const PREVIEW_CHARACTERS = 5
const PREVIEW_SHOTS = 3

const intros = computed(() => {
  const seen = new Set<string>()
  return (props.work.intros ?? []).flatMap((i) => {
    const text = i.value.replace(/\\\n/g, '\n').trim()
    if (!text || seen.has(text)) return []
    seen.add(text)
    return [{ ...i, text }]
  })
})
const introTabs = computed<KunTabItem[]>(() =>
  intros.value.map((i, idx) => ({
    value: String(idx),
    textValue: `${vocabLabel('intro_lang', i.lang)}${i.is_machine ? ' · 机翻' : ''}`
  }))
)
const introIdx = ref('0')
watch(
  intros,
  (list) => {
    const zh = list.findIndex((i) => i.lang.startsWith('zh'))
    introIdx.value = String(Math.max(zh, 0))
  },
  { immediate: true }
)
const intro = computed(() => intros.value[Number(introIdx.value)] ?? null)

const tags = computed(() => {
  const byName = new Map<string, ExploreTag>()
  for (const t of props.work.tags ?? []) {
    if ((t.spoiler ?? 'none') !== 'none' || t.tier === 'hidden') continue
    if (!byName.has(t.display_name)) byName.set(t.display_name, t)
  }
  return [...byName.values()].sort(
    (a, b) => Number(b.tier === 'core') - Number(a.tier === 'core')
  )
})
const showAllTags = ref(false)
const visibleTags = computed(() =>
  showAllTags.value ? tags.value : tags.value.slice(0, TAG_CAP)
)

const mainCharacters = computed(() =>
  (props.work.characters ?? []).filter(
    (c) => c.roster_role === 'main' && (c.spoiler ?? 'none') === 'none'
  )
)
const shots = computed(() =>
  (props.work.screenshots ?? []).filter((s) => isImageShown(s, props.nsfw))
)
</script>

<template>
  <div class="space-y-10">
    <ExploreWorkSection v-if="intro" title="简介" icon="lucide:scroll-text">
      <template v-if="introTabs.length > 1" #action>
        <KunTab
          v-model="introIdx"
          :items="introTabs"
          variant="pills"
          size="sm"
          aria-label="简介语言"
        />
      </template>
      <div
        class="border-default-200 bg-content1 space-y-3 rounded-xl border p-5"
      >
        <p class="text-default-600 text-[15px] leading-7 whitespace-pre-line">
          {{ intro.text }}
        </p>
        <p
          v-if="intro.source || intro.is_machine"
          class="text-default-400 flex flex-wrap gap-2 text-xs"
        >
          <span v-if="intro.source"
            >来源：{{ vocabLabel('sources', intro.source) }}</span
          >
          <span v-if="intro.is_machine" class="text-warning">
            机器翻译（API 原样标注 is_machine=true）
          </span>
        </p>
      </div>
    </ExploreWorkSection>

    <ExploreWorkSection
      v-if="(work.ratings ?? []).length"
      title="评分"
      icon="lucide:star"
    >
      <ExploreWorkRatings :ratings="work.ratings ?? []" />
    </ExploreWorkSection>

    <ExploreWorkSection
      v-if="(work.popularity ?? []).some((p) => p.value > 0)"
      title="玩家数据"
      icon="lucide:chart-column"
    >
      <ExploreWorkPopularity :popularity="work.popularity ?? []" />
    </ExploreWorkSection>

    <ExploreWorkSection v-if="tags.length" title="标签" icon="lucide:tags">
      <div class="flex flex-wrap gap-1.5">
        <KunChip
          v-for="t in visibleTags"
          :key="t.display_name"
          :color="
            t.is_sexual ? 'danger' : t.tier === 'core' ? 'primary' : 'default'
          "
          variant="flat"
          size="sm"
        >
          {{ t.display_name }}
        </KunChip>
        <KunButton
          v-if="tags.length > TAG_CAP"
          variant="light"
          color="primary"
          size="sm"
          @click="showAllTags = !showAllTags"
        >
          {{ showAllTags ? '收起' : `全部 ${tags.length} 个` }}
        </KunButton>
      </div>
    </ExploreWorkSection>

    <ExploreWorkSection
      v-if="mainCharacters.length"
      title="主要角色"
      icon="lucide:users"
    >
      <template #action>
        <KunButton
          variant="light"
          color="primary"
          size="sm"
          @click="emit('go', 'characters')"
        >
          全部角色
          <KunIcon name="lucide:arrow-right" class="size-4" />
        </KunButton>
      </template>
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-5">
        <ExploreWorkCharacterCard
          v-for="c in mainCharacters.slice(0, PREVIEW_CHARACTERS)"
          :key="c.id"
          :character="c"
          :nsfw="nsfw"
          @open="emit('open', $event)"
        />
      </div>
    </ExploreWorkSection>

    <ExploreWorkSection v-if="shots.length" title="截图" icon="lucide:image">
      <template #action>
        <KunButton
          variant="light"
          color="primary"
          size="sm"
          @click="emit('go', 'gallery')"
        >
          打开图集
          <KunIcon name="lucide:arrow-right" class="size-4" />
        </KunButton>
      </template>
      <KunLightboxGallery>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <KunLightboxGalleryItem
            v-for="(s, i) in shots.slice(0, PREVIEW_SHOTS)"
            :key="s.url"
            as="div"
            :src="s.url"
            :alt="`${work.display_name} 截图 ${i + 1}`"
            class="group border-default-200 bg-default-100 overflow-hidden rounded-xl border"
          >
            <KunImageNative
              :src="s.url"
              :alt="`截图 ${i + 1}`"
              loading="lazy"
              class-name="aspect-video w-full object-cover transition-transform duration-300 group-hover:scale-[1.03]"
            />
          </KunLightboxGalleryItem>
        </div>
      </KunLightboxGallery>
    </ExploreWorkSection>
  </div>
</template>
