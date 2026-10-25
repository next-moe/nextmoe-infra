<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WorkspaceSectionDef } from '~/constants/workspace'
import type { CatalogWalkthrough } from '#shared/types/catalog'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const SOURCE_LABELS: Record<string, string> = {
  seiya: '誠也の部屋',
  sagaoz_foolmaker: '愚者の館',
  nextmoe: '本站撰写'
}

const nav = useWorkspaceNav()
const api = useEditorApi()
const { entityId } = useEditorContext()

const items = ref<CatalogWalkthrough[]>([])
const loaded = ref(false)
const refreshing = ref(false)
const query = ref('')

const load = async () => {
  const page = await api<{ items: CatalogWalkthrough[] }>(
    '/v2/catalog/walkthroughs',
    { query: { work_id: entityId.value, nsfw: true, limit: 100 } }
  ).catch(() => null)
  items.value = page?.items ?? []
  loaded.value = true
}

const refresh = async () => {
  refreshing.value = true
  await load()
  refreshing.value = false
}

const sourceOf = (item: CatalogWalkthrough) =>
  SOURCE_LABELS[item.source] ?? item.source

const titleOf = (item: CatalogWalkthrough) =>
  item.characters.map((c) => c.display_name || c.name).join('、') ||
  `攻略 #${item.id}`

const metaOf = (item: CatalogWalkthrough) =>
  [
    sourceOf(item),
    `${item.segment_count} 段`,
    `${item.ending_count} 个结局`,
    item.save_count ? `${item.save_count} 个存档` : ''
  ]
    .filter(Boolean)
    .join(' · ')

const filtered = computed(() =>
  items.value.filter((item) =>
    matchesFilter(query.value, titleOf(item), item.id, metaOf(item))
  )
)

const selected = computed(
  () => items.value.find((item) => item.id === nav.item.value) ?? null
)
const select = (item: CatalogWalkthrough | null) => {
  nav.item.value = item?.id ?? null
}
useAutoSelectFirst(selected, () => filtered.value[0], select)

onMounted(load)
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="攻略列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        :total="items.length"
        placeholder="按角色、来源筛选"
        :can-add="false"
      >
        <template #actions>
          <KunTooltip text="在新标签页写完攻略后点这里刷新" position="bottom">
            <KunButton
              size="sm"
              variant="light"
              is-icon-only
              aria-label="刷新攻略列表"
              :loading="refreshing"
              @click="refresh"
            >
              <KunIcon name="lucide:refresh-cw" class="size-4" />
            </KunButton>
          </KunTooltip>
          <KunButton
            :href="`/walkthroughs/new?work_id=${entityId}`"
            target="_blank"
            size="sm"
            variant="light"
            color="primary"
          >
            <KunIcon name="lucide:plus" class="size-4" />
            新建攻略
          </KunButton>
        </template>
      </WorkspaceRelatedListHeader>
    </template>

    <template #list>
      <WorkspaceSkeletonList v-if="!loaded" :rows="4" />
      <WorkspaceEmpty
        v-else-if="!items.length"
        icon="lucide:route"
        title="还没有攻略"
        description="用右上角的「新建攻略」为这部作品写一份选项图。"
      />
      <WorkspaceEmpty
        v-else-if="!filtered.length"
        icon="lucide:search-x"
        title="没有匹配的攻略"
      />
      <WorkspaceRelatedRow
        v-for="item in filtered"
        :key="item.id"
        :title="titleOf(item)"
        :subtitle="metaOf(item)"
        :selected="selected?.id === item.id"
        icon="lucide:route"
        @select="select(item)"
      />
    </template>

    <template #detail>
      <div
        v-if="selected"
        :key="selected.id"
        class="mx-auto w-full max-w-3xl space-y-5 px-4 py-5 md:px-8 md:py-6"
      >
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0 space-y-1">
            <h2 class="text-foreground truncate text-lg font-semibold">
              {{ titleOf(selected) }}
            </h2>
            <p class="text-default-500 text-sm">{{ metaOf(selected) }}</p>
          </div>
          <KunButton
            :href="`/walkthroughs/${selected.id}/edit`"
            target="_blank"
            size="sm"
            color="primary"
          >
            <KunIcon name="lucide:pencil-line" class="size-4" />
            打开编辑器
          </KunButton>
        </div>

        <div class="flex flex-wrap gap-2">
          <span
            v-for="character in selected.characters"
            :key="character.key"
            :class="
              cn(
                'rounded-full px-2.5 py-0.5 text-xs',
                character.character_id
                  ? 'bg-default-100 text-default-700'
                  : 'bg-warning-100 text-warning-700'
              )
            "
          >
            {{ character.display_name || character.name }}
            <template v-if="!character.character_id">· 未关联</template>
          </span>
        </div>

        <dl class="text-default-500 grid grid-cols-[6rem_1fr] gap-y-2 text-sm">
          <dt>来源</dt>
          <dd class="text-foreground">
            <a
              v-if="selected.source_url"
              :href="selected.source_url"
              target="_blank"
              rel="noopener"
              class="hover:underline"
            >
              {{ sourceOf(selected) }}
            </a>
            <template v-else>{{ sourceOf(selected) }}</template>
            <span v-if="selected.source_author" class="text-default-500">
              · {{ selected.source_author }}
            </span>
          </dd>
          <dt>语言</dt>
          <dd class="text-foreground">
            {{ selected.lang === 'zh-Hans' ? '简体中文' : '日文原文' }}
          </dd>
          <dt>最后更新</dt>
          <dd class="text-foreground">
            {{ selected.updated_at.slice(0, 10) }}
          </dd>
        </dl>
      </div>
    </template>
  </WorkspaceListDetail>
</template>
