<script setup lang="ts">
import type { KunDropdownItem, KunTabItem } from '@kungal/ui-vue'
import type { CatalogWork } from '#shared/types/catalog'
import { WORK_DETAIL_INCLUDE } from '~/constants/catalog'
import { deletedProblem } from '~/utils/review/problem'

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

const {
  data: work,
  error,
  refresh
} = useLazyAsyncData(
  () => `work-${id.value}`,
  () =>
    api<CatalogWork>(`/v2/catalog/works/${id.value}`, {
      query: { nsfw: true, view: 'full', include: WORK_DETAIL_INCLUDE }
    }),
  { watch: [id] }
)

const mergeOpen = ref(false)
const deleteOpen = ref(false)
const deleted = computed(() => deletedProblem(error.value))

const moreItems: KunDropdownItem[] = [
  {
    key: 'delete',
    label: '申请删除',
    icon: 'lucide:trash-2',
    color: 'danger'
  }
]
const onMore = (item: KunDropdownItem) => {
  if (item.key === 'delete') {
    deleteOpen.value = true
  }
}

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
      <div v-if="!error" class="flex flex-wrap justify-end gap-2">
        <KunButton
          :href="`/new/release?work_id=${id}`"
          variant="light"
          size="sm"
        >
          <KunIcon name="lucide:package-plus" class="size-4" />
          新建发行
        </KunButton>
        <KunButton
          variant="light"
          size="sm"
          :disabled="!work"
          @click="mergeOpen = true"
        >
          <KunIcon name="lucide:git-merge" class="size-4" />
          合并
        </KunButton>
        <KunButton
          :href="`/works/${id}/edit`"
          target="_blank"
          color="primary"
          size="sm"
        >
          <KunIcon name="lucide:pencil" class="size-4" />
          编辑资料
        </KunButton>
        <KunDropdown
          :items="moreItems"
          position="bottom-end"
          :disabled="!work"
          @select="onMore"
        >
          <template #trigger>
            <KunButton
              variant="light"
              size="sm"
              is-icon-only
              aria-label="更多操作"
              :disabled="!work"
            >
              <KunIcon name="lucide:ellipsis" class="size-4" />
            </KunButton>
          </template>
        </KunDropdown>
      </div>
    </div>
    <DeleteRequestModal
      v-if="work"
      v-model="deleteOpen"
      :work-id="String(id)"
      :work-name="work.display_name"
    />
    <MergeRequestModal
      v-if="work"
      v-model="mergeOpen"
      family="work"
      :entity-id="String(id)"
      :entity-name="work.display_name"
    />

    <DeleteDeletedState
      v-if="deleted"
      :work-id="String(id)"
      :reason="deleted.reason"
      :deleted-at="deleted.deletedAt"
      @restored="refresh()"
    />
    <KunInfo
      v-else-if="error"
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

    <CommonDetailSkeleton v-else media="cover" :tabs="TABS.length" />
  </div>
</template>
