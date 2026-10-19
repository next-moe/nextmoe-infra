<script setup lang="ts">
import type { KunTabItem } from '@kungal/ui-vue'
import type { CatalogWork } from '#shared/types/catalog'
import { WORK_DETAIL_INCLUDE } from '~/constants/catalog'

const TABS: KunTabItem[] = [
  { value: 'overview', textValue: '概览', icon: 'lucide:info' },
  { value: 'intros', textValue: '简介', icon: 'lucide:align-left' },
  { value: 'tags', textValue: '标签', icon: 'lucide:tags' },
  { value: 'relations', textValue: '关联', icon: 'lucide:git-fork' },
  { value: 'media', textValue: '媒体', icon: 'lucide:images' },
  { value: 'people', textValue: '人员与角色', icon: 'lucide:users' },
  { value: 'links', textValue: '外链', icon: 'lucide:link' },
  { value: 'edit', textValue: '编辑', icon: 'lucide:pencil' }
]

const api = useEditorApi()
const route = useRoute()
const router = useRouter()

const id = computed(() => String(route.params.id))
const tab = ref(
  typeof route.query.tab === 'string' &&
    TABS.some((t) => t.value === route.query.tab)
    ? route.query.tab
    : 'overview'
)

watch(tab, (value) => {
  router.replace({
    query: { ...route.query, tab: value === 'overview' ? undefined : value }
  })
})

const { data: work, error } = await useAsyncData(
  () => `work-${id.value}`,
  () =>
    api<CatalogWork>(`/v2/catalog/works/${id.value}`, {
      query: { nsfw: true, view: 'full', include: WORK_DETAIL_INCLUDE }
    }),
  { watch: [id] }
)

useHead(() => ({
  title: work.value
    ? `${work.value.display_name} · 未萌编辑台`
    : '作品 · 未萌编辑台'
}))
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-5">
    <div class="flex items-center justify-between gap-2">
      <NuxtLink
        to="/works"
        class="text-default-500 hover:text-foreground inline-flex items-center gap-1 text-sm"
      >
        <KunIcon name="lucide:chevron-left" class="size-4" />
        作品
      </NuxtLink>
      <KunButton
        v-if="work"
        :href="`/works/${id}/edit`"
        color="primary"
        size="sm"
      >
        <KunIcon name="lucide:pencil" class="size-4" />
        编辑资料
      </KunButton>
    </div>

    <KunInfo
      v-if="error"
      color="danger"
      variant="flat"
      title="无法加载这部作品"
      description="它可能已被合并、删除，或资料库暂时不可用。"
    />

    <template v-else-if="work">
      <WorksDetailHeader :work="work" />

      <KunTab
        v-model="tab"
        :items="TABS"
        variant="underlined"
        name="work-detail"
      />

      <KunTabPanels v-model="tab" mount="lazy" name="work-detail">
        <KunTabPanel value="overview"
          ><WorksDetailOverview :work="work"
        /></KunTabPanel>
        <KunTabPanel value="intros"
          ><WorksDetailIntros :work="work"
        /></KunTabPanel>
        <KunTabPanel value="tags"><WorksDetailTags :work="work" /></KunTabPanel>
        <KunTabPanel value="relations"
          ><WorksDetailRelations :work="work"
        /></KunTabPanel>
        <KunTabPanel value="media"
          ><WorksDetailMedia :work="work"
        /></KunTabPanel>
        <KunTabPanel value="people"
          ><WorksDetailPeople :work="work"
        /></KunTabPanel>
        <KunTabPanel value="links"
          ><WorksDetailLinks :work="work"
        /></KunTabPanel>
        <KunTabPanel value="edit"
          ><WorksDetailEditPreview :work-id="id"
        /></KunTabPanel>
      </KunTabPanels>
    </template>
  </div>
</template>
