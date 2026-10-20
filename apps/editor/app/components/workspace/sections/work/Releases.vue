<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'
import { RELEASE_KIND_LABELS, RELEASE_KIND_ORDER } from '~/constants/related'
import { vocabLabel } from '~/constants/vocabLabels'
import type { RelatedRelease } from '~/composables/useWorkRelated'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const nav = useWorkspaceNav()
const { entityId } = useEditorContext()
const related = useWorkRelated(entityId)
const vocabularies = useVocabularies()

const query = ref('')
const refreshing = ref(false)

const platformName = (token: string) =>
  vocabularies.byName.value.platform?.values.find(
    (entry) => entry.value === token
  )?.display_name ?? token

const releases = computed(() =>
  [...related.display.value.releases].sort((a, b) =>
    (a.date ?? '9999').localeCompare(b.date ?? '9999')
  )
)

const metaOf = (release: RelatedRelease) =>
  [
    release.date,
    release.platforms.map(platformName).join(' / '),
    (release.languages.length ? release.languages : [release.lang])
      .filter(Boolean)
      .map((lang) => vocabLabel('release_lang', lang))
      .join('、')
  ]
    .filter(Boolean)
    .join(' · ')

const filtered = computed(() =>
  releases.value.filter((release) =>
    matchesFilter(
      query.value,
      release.title,
      release.id,
      metaOf(release),
      RELEASE_KIND_LABELS[release.kind]
    )
  )
)
const groups = computed(() =>
  groupRows(
    filtered.value.map((release) => ({
      key: release.id,
      origin: 'upstream' as const,
      value: release,
      index: -1,
      identity: null,
      source: '',
      state: null
    })),
    (row) => row.value.kind,
    RELEASE_KIND_ORDER
  )
)

const selected = computed(
  () => releases.value.find((release) => release.id === nav.item.value) ?? null
)
const select = (release: RelatedRelease | null) => {
  nav.item.value = release?.id ?? null
}
useAutoSelectFirst(selected, () => groups.value[0]?.items[0]?.value, select)

const refresh = async () => {
  refreshing.value = true
  await related.load(true)
  refreshing.value = false
}

onMounted(() => {
  related.load()
  vocabularies.load(['platform', 'release_lang'])
})
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="发行列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        :total="releases.length"
        placeholder="按标题、平台、语言筛选"
        :can-add="false"
      >
        <template #actions>
          <KunTooltip text="新建发行后点这里刷新列表" position="bottom">
            <KunButton
              size="sm"
              variant="light"
              is-icon-only
              aria-label="刷新发行列表"
              :loading="refreshing"
              @click="refresh"
            >
              <KunIcon name="lucide:refresh-cw" class="size-4" />
            </KunButton>
          </KunTooltip>
          <KunButton
            :href="`/new/release?work_id=${entityId}`"
            target="_blank"
            size="sm"
            variant="light"
            color="primary"
          >
            <KunIcon name="lucide:plus" class="size-4" />
            新建发行
          </KunButton>
        </template>
      </WorkspaceRelatedListHeader>
    </template>

    <template #list>
      <WorkspaceSkeletonList v-if="!related.display.value.loaded" :rows="6" />
      <WorkspaceEmpty
        v-else-if="!releases.length"
        icon="lucide:package"
        title="还没有发行版本"
        description="用右上角的「新建发行」添加这部作品的发行版本。"
      />
      <WorkspaceEmpty
        v-else-if="!filtered.length"
        icon="lucide:search-x"
        title="没有匹配的发行"
      />

      <div v-for="group in groups" :key="group.key">
        <WorkspaceRelatedGroup
          :label="RELEASE_KIND_LABELS[group.key] ?? group.key"
          :count="group.items.length"
        />
        <WorkspaceRelatedRow
          v-for="row in group.items"
          :key="row.key"
          :title="row.value.title || `发行 #${row.value.id}`"
          :subtitle="metaOf(row.value)"
          :selected="selected?.id === row.value.id"
          icon="lucide:package"
          @select="select(row.value)"
        >
          <template #trailing>
            <span
              v-if="row.value.freeware"
              class="bg-success-100 text-success-700 rounded px-1.5 text-[11px] leading-5"
            >
              免费
            </span>
            <span
              v-if="!row.value.official"
              class="bg-warning-100 text-warning-700 rounded px-1.5 text-[11px] leading-5"
            >
              非官方
            </span>
            <span
              v-if="row.value.minAge && row.value.minAge >= 18"
              class="bg-danger-100 text-danger-700 rounded px-1.5 text-[11px] leading-5"
            >
              18+
            </span>
          </template>
        </WorkspaceRelatedRow>
      </div>
    </template>

    <template #detail>
      <WorkspaceEntityPanel
        v-if="selected"
        :key="selected.id"
        family="release"
        :entity-id="selected.id"
        :label="selected.title"
        :subtitle="`${RELEASE_KIND_LABELS[selected.kind] ?? '发行'} · ${metaOf(selected) || `#${selected.id}`}`"
      />
    </template>
  </WorkspaceListDetail>
</template>
