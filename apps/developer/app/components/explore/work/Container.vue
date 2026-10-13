<script setup lang="ts">
import { EXPLORE_WORK_INCLUDE } from '~/constants/explore'
import type { KunTabItem } from '@kungal/ui-vue'
import type { ExploreEntityTarget, ExploreWork } from '~~/shared/types/explore'

const route = useRoute()
const workId = computed(() => String(route.params.id))
const nsfw = computed(() => route.query.nsfw === '1')

const apiKey = ref('')
const loading = ref(true)
const error = ref('')
const work = ref<ExploreWork | null>(null)
const entityTarget = ref<ExploreEntityTarget | null>(null)
const tab = ref('overview')

useSeoMeta({
  title: () => work.value?.display_name ?? '作品预览',
  robots: 'noindex'
})

const { read, failureMessage } = useCatalogRead()

const load = async () => {
  loading.value = true
  error.value = ''
  entityTarget.value = null
  try {
    work.value = await read<ExploreWork>(
      `v2/catalog/works/${workId.value}`,
      { include: EXPLORE_WORK_INCLUDE, ...(nsfw.value && { nsfw: 'true' }) },
      apiKey.value
    )
  } catch (e) {
    work.value = null
    error.value = failureMessage(e, apiKey.value)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  apiKey.value = sessionStorage.getItem('explore_api_key') ?? ''
  load()
})

watch(nsfw, load)

const setNsfw = (on: boolean) =>
  navigateTo(
    { query: { ...route.query, nsfw: on ? '1' : undefined } },
    { replace: true }
  )

const openEntity = (target: ExploreEntityTarget) => {
  entityTarget.value = target
}

const shownCharacters = computed(
  () =>
    (work.value?.characters ?? []).filter(
      (c) => (c.spoiler ?? 'none') === 'none'
    ).length
)
const staffCount = computed(
  () =>
    new Set(
      (work.value?.credits ?? []).flatMap((g) => g.credits.map((c) => c.id))
    ).size
)
const galleryImages = computed(() => [
  ...(work.value?.screenshots ?? []),
  ...(work.value?.covers ?? [])
])

const tabs = computed(() => {
  const w = work.value
  const all: (KunTabItem & { count?: number; present: boolean })[] = [
    {
      value: 'overview',
      textValue: '概览',
      icon: 'lucide:book-open',
      present: true
    },
    {
      value: 'characters',
      textValue: '角色',
      icon: 'lucide:users',
      count: shownCharacters.value,
      present: (w?.characters?.length ?? 0) > 0
    },
    {
      value: 'staff',
      textValue: '制作人员',
      icon: 'lucide:clapperboard',
      count: staffCount.value,
      present: staffCount.value > 0
    },
    {
      value: 'releases',
      textValue: '发行版本',
      icon: 'lucide:package',
      count: w?.releases?.length ?? 0,
      present: (w?.releases?.length ?? 0) > 0
    },
    {
      value: 'gallery',
      textValue: '图集',
      icon: 'lucide:images',
      count: galleryImages.value.filter((i) => isImageShown(i, nsfw.value))
        .length,
      present: galleryImages.value.length > 0
    },
    {
      value: 'relations',
      textValue: '关联作品',
      icon: 'lucide:git-fork',
      count: (w?.relations?.length ?? 0) + (w?.series?.length ?? 0),
      present: (w?.relations?.length ?? 0) + (w?.series?.length ?? 0) > 0
    }
  ]
  return all.filter((t) => t.present)
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <nav class="text-default-500 flex min-w-0 items-center gap-1.5 text-sm">
        <NuxtLink
          to="/explore"
          class="hover:text-foreground inline-flex shrink-0 items-center gap-1.5 transition-colors"
        >
          <KunIcon name="lucide:database" class="size-4" />
          数据浏览
        </NuxtLink>
        <KunIcon
          name="lucide:chevron-right"
          class="text-default-300 size-3.5"
        />
        <span class="text-foreground truncate">
          {{ work?.display_name ?? `作品 #${workId}` }}
        </span>
      </nav>
      <KunSwitch
        :model-value="nsfw"
        size="sm"
        label="显示 R18 内容"
        @update:model-value="setNsfw"
      />
    </div>

    <div v-if="loading" class="space-y-6">
      <KunSkeleton variant="rect" height="18rem" rounded="lg" />
      <div class="grid grid-cols-1 gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div class="space-y-3">
          <KunSkeleton variant="text" width="40%" height="1.5rem" />
          <KunSkeleton variant="text" />
          <KunSkeleton variant="text" />
          <KunSkeleton variant="text" width="70%" />
        </div>
        <KunSkeleton variant="rect" height="20rem" rounded="lg" />
      </div>
    </div>

    <KunInfo
      v-else-if="error"
      color="danger"
      variant="flat"
      icon="lucide:circle-alert"
      title="没能取到这部作品"
      :description="error"
    />

    <template v-else-if="work">
      <ExploreWorkHero :work="work" :nsfw="nsfw" @open="openEntity" />

      <div
        class="grid grid-cols-1 items-start gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]"
      >
        <aside class="min-w-0 space-y-4 lg:col-start-2 lg:row-start-1">
          <ExploreWorkInfobox :work="work" @open="openEntity" />
        </aside>

        <div class="min-w-0 space-y-6 lg:col-start-1 lg:row-start-1">
          <div
            class="border-default-200 bg-background/90 sticky top-16 z-20 -mx-1 border-b px-1 backdrop-blur-md"
          >
            <KunTab
              v-model="tab"
              :items="tabs"
              variant="underlined"
              color="primary"
              aria-label="作品资料分区"
              inner-class-name="gap-1"
            >
              <template #tab="{ item }">
                <KunIcon :name="item.icon ?? ''" class="size-4" />
                <span>{{ item.textValue }}</span>
                <span v-if="item.count" class="text-default-400 text-xs">
                  {{ item.count }}
                </span>
              </template>
            </KunTab>
          </div>

          <KunTabPanels v-model="tab" mount="lazy">
            <KunTabPanel value="overview">
              <ExploreWorkOverview
                :work="work"
                :nsfw="nsfw"
                @open="openEntity"
                @go="tab = $event"
              />
            </KunTabPanel>
            <KunTabPanel value="characters">
              <ExploreWorkCharacters
                :characters="work.characters ?? []"
                :nsfw="nsfw"
                @open="openEntity"
              />
            </KunTabPanel>
            <KunTabPanel value="staff">
              <ExploreWorkStaff :work="work" @open="openEntity" />
            </KunTabPanel>
            <KunTabPanel value="releases">
              <ExploreWorkReleases :releases="work.releases ?? []" />
            </KunTabPanel>
            <KunTabPanel value="gallery">
              <ExploreWorkGallery
                :work="work"
                :nsfw="nsfw"
                @nsfw="setNsfw(true)"
              />
            </KunTabPanel>
            <KunTabPanel value="relations">
              <ExploreWorkRelations :work="work" :nsfw="nsfw" />
            </KunTabPanel>
          </KunTabPanels>
        </div>
      </div>

      <p
        class="border-default-200 text-default-400 border-t pt-4 text-xs leading-relaxed"
      >
        本页由 NextMoe 公开 API v2 实时渲染，只用一次
        <code class="font-mono">GET /v2/catalog/works/{id}</code>
        ——
        封面、标签、评分、时长、热度、角色、制作人员、发行版本、关联作品全部经
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

    <ExploreEntityModal v-model="entityTarget" :api-key="apiKey" :nsfw="nsfw" />
  </div>
</template>
