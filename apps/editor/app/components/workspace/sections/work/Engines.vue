<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WorkspaceSectionDef } from '~/constants/workspace'
import type { RelatedIdRow } from '~/utils/relatedRows'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const ENGINES = 'catalog.work.engine_ids'
const PLATFORMS = 'catalog.work.platforms'

const nav = useWorkspaceNav()
const { entityId, refs, editor } = useEditorContext()
const related = useWorkRelated(entityId)
const engines = useRelatedField(ENGINES)
const platforms = useRelatedField(PLATFORMS)
const vocabularies = useVocabularies()

const query = ref('')
const adding = ref(false)
const pickEngine = ref<string | null>(null)
const pickPlatform = ref<string | null>(null)

const hasPlatforms = computed(() => editor.fieldByKey.value.has(PLATFORMS))
const hasEngines = computed(() => editor.fieldByKey.value.has(ENGINES))

const platformNames = computed(
  () =>
    new Map(
      (vocabularies.byName.value.platform?.values ?? []).map((entry) => [
        entry.value,
        entry.display_name
      ])
    )
)
const platformLabel = (token: string) =>
  platformNames.value.get(token) ?? token.toUpperCase()

const platformRows = computed(() =>
  mergeIdRows({
    human: platforms.list.value,
    base: platforms.baseList.value,
    context: platforms.contextRows.value,
    suppressed: platforms.suppressed.value
  })
)
const livePlatforms = computed(() =>
  platformRows.value.filter((row) => row.state !== 'hidden')
)
const hiddenPlatforms = computed(() =>
  platformRows.value.filter((row) => row.state === 'hidden')
)
const platformOptions = computed(() => {
  const taken = new Set(platformRows.value.map((row) => row.id))
  return (vocabularies.byName.value.platform?.values ?? [])
    .filter((entry) => !taken.has(entry.value))
    .map((entry) => ({ value: entry.value, label: entry.display_name }))
})

const togglePlatform = (row: RelatedIdRow) => {
  if (row.origin === 'human') {
    platforms.removeAt(row.index)
  } else if (row.identity) {
    platforms.toggleSuppressed(row.identity)
  }
}

watch(pickPlatform, (token) => {
  if (!token) {
    return
  }
  pickPlatform.value = null
  if (!platformRows.value.some((row) => row.id === token)) {
    platforms.append(token)
  }
})

const engineRows = computed(() =>
  mergeIdRows({
    human: engines.list.value,
    base: engines.baseList.value,
    context: engines.contextRows.value,
    suppressed: engines.suppressed.value
  }).map((row) => ({
    ...row,
    name:
      related.display.value.engines[row.id]?.name ||
      refs.nameOf('engine', row.id) ||
      `引擎 #${row.id}`,
    known:
      related.display.value.engines[row.id]?.name ||
      refs.nameOf('engine', row.id)
  }))
)
type EngineRow = (typeof engineRows.value)[number]

const filteredEngines = computed(() =>
  engineRows.value.filter((row) => matchesFilter(query.value, row.name, row.id))
)
const visibleEngines = computed(() =>
  filteredEngines.value.filter((row) => row.state !== 'hidden')
)
const hiddenEngines = computed(() =>
  filteredEngines.value.filter((row) => row.state === 'hidden')
)

const selected = computed(
  () => engineRows.value.find((row) => row.key === nav.item.value) ?? null
)
const select = (row: EngineRow | null) => {
  nav.item.value = row?.key ?? null
}
useAutoSelectFirst(selected, () => visibleEngines.value[0], select)

const removeEngine = (row: EngineRow) => {
  if (row.origin === 'human') {
    engines.removeAt(row.index)
    select(null)
  } else if (row.identity) {
    engines.toggleSuppressed(row.identity)
  }
}

watch(pickEngine, (id) => {
  if (!id) {
    return
  }
  pickEngine.value = null
  adding.value = false
  const existing = engineRows.value.find((row) => row.id === id)
  if (existing) {
    select(existing)
    return
  }
  const index = engines.append(Number(id))
  nav.item.value = humanRowKey(index)
})

watch(
  () => engineRows.value.map((row) => row.id).join(','),
  (joined) => refs.resolve('engine', joined ? joined.split(',') : []),
  { immediate: true }
)

onMounted(() => {
  related.load()
  vocabularies.load(['platform'])
})
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="引擎列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        placeholder="按引擎名筛选"
        add-label="添加引擎"
        :can-add="hasEngines && !engines.disabled.value"
        :adding="adding"
        @add="adding = !adding"
      />
    </template>

    <template #list>
      <section
        v-if="hasPlatforms"
        class="border-default-200 space-y-2.5 border-b px-3 py-3"
      >
        <div class="flex items-center gap-2">
          <h3 class="text-sm font-semibold">平台</h3>
          <span class="text-default-400 text-xs tabular-nums">
            {{ livePlatforms.length }}
          </span>
        </div>
        <div v-if="livePlatforms.length" class="flex flex-wrap gap-1.5">
          <span
            v-for="row in livePlatforms"
            :key="row.key"
            :class="
              cn(
                'inline-flex h-7 items-center gap-1 rounded-full border pr-1 pl-2.5 text-sm',
                row.state === 'added'
                  ? 'border-success-300 bg-success-50'
                  : 'border-default-200 bg-content1'
              )
            "
          >
            {{ platformLabel(row.id) }}
            <span
              v-if="row.origin === 'upstream'"
              class="text-default-400 text-[11px]"
            >
              {{ row.source }}
            </span>
            <KunTooltip
              :text="row.origin === 'human' ? '删除' : '隐藏'"
              position="top"
            >
              <button
                v-if="!platforms.disabled.value"
                type="button"
                class="text-default-400 hover:bg-danger-50 hover:text-danger-600 flex size-5 items-center justify-center rounded-full"
                :aria-label="`${row.origin === 'human' ? '删除' : '隐藏'}${platformLabel(row.id)}`"
                @click="togglePlatform(row)"
              >
                <KunIcon name="lucide:x" class="size-3" />
              </button>
            </KunTooltip>
          </span>
        </div>
        <p v-else class="text-default-400 text-xs">还没有作品层面的平台。</p>
        <div v-if="hiddenPlatforms.length" class="flex flex-wrap gap-1.5">
          <button
            v-for="row in hiddenPlatforms"
            :key="row.key"
            type="button"
            class="border-default-200 text-default-400 hover:text-primary-600 inline-flex h-6 items-center gap-1 rounded-full border border-dashed px-2 text-xs line-through"
            :disabled="platforms.disabled.value"
            @click="togglePlatform(row)"
          >
            {{ platformLabel(row.id) }}
          </button>
          <span class="text-default-400 self-center text-xs"
            >已隐藏，点击恢复</span
          >
        </div>
        <KunSelect
          v-if="!platforms.disabled.value"
          v-model="pickPlatform"
          :options="platformOptions"
          placeholder="添加平台"
          aria-label="添加平台"
          searchable
          size="sm"
        />
        <p class="text-default-400 text-xs leading-5">
          这里是作品层面的平台；各发行版本自己的平台在「发行」里编辑。
        </p>
      </section>

      <template v-if="hasEngines">
        <WorkspaceRelatedAddPanel v-if="adding" title="添加这部作品使用的引擎">
          <EditRefPicker
            v-model="pickEngine"
            family="engine"
            :exclude="engineRows.map((row) => row.id)"
            aria-label="搜索引擎"
            placeholder="搜索引擎名或输入 ID"
          />
          <KunButton
            href="/new/engine"
            target="_blank"
            size="xs"
            variant="light"
          >
            <KunIcon name="lucide:external-link" class="size-3.5" />
            新建引擎
          </KunButton>
        </WorkspaceRelatedAddPanel>

        <WorkspaceRelatedGroup label="引擎" :count="visibleEngines.length" />
        <WorkspaceEmpty
          v-if="!engineRows.length"
          icon="lucide:cpu"
          title="还没有记录引擎"
          description="用右上角的「添加引擎」记录这部作品使用的游戏引擎。"
        />
        <WorkspaceRelatedRow
          v-for="row in visibleEngines"
          :key="row.key"
          :title="row.name"
          :subtitle="`#${row.id}`"
          :selected="selected?.key === row.key"
          :state="row.state"
          icon="lucide:cpu"
          :source="row.origin === 'upstream' ? row.source : ''"
          :human="row.origin === 'human'"
          @select="select(row)"
        />
        <WorkspaceRelatedHiddenGroup
          :count="hiddenEngines.length"
          label="已隐藏的引擎"
        >
          <WorkspaceRelatedRow
            v-for="row in hiddenEngines"
            :key="row.key"
            :title="row.name"
            :selected="selected?.key === row.key"
            :state="row.state"
            icon="lucide:cpu"
            :source="row.source"
            @select="select(row)"
          />
        </WorkspaceRelatedHiddenGroup>
      </template>
    </template>

    <template #detail>
      <WorkspaceEntityPanel
        v-if="selected"
        :key="selected.id"
        family="engine"
        :entity-id="selected.id"
        :label="selected.known"
        :subtitle="`引擎 · #${selected.id}`"
      >
        <template #context>
          <WorkspaceRelatedContextCard
            :origin="selected.origin"
            :source="selected.source"
            :state="selected.state"
            :note="
              selected.origin === 'upstream'
                ? '资料源导入的引擎不能删除，不正确时可以在本作中隐藏。'
                : ''
            "
          >
            <template #actions>
              <KunButton
                size="xs"
                variant="light"
                :color="selected.state === 'hidden' ? 'primary' : 'danger'"
                :disabled="engines.disabled.value"
                @click="removeEngine(selected)"
              >
                <KunIcon
                  :name="
                    selected.origin === 'human'
                      ? 'lucide:trash-2'
                      : selected.state === 'hidden'
                        ? 'lucide:eye'
                        : 'lucide:eye-off'
                  "
                  class="size-3.5"
                />
                {{
                  selected.origin === 'human'
                    ? '从本作移除'
                    : selected.state === 'hidden'
                      ? '恢复显示'
                      : '在本作中隐藏'
                }}
              </KunButton>
            </template>
          </WorkspaceRelatedContextCard>
        </template>
      </WorkspaceEntityPanel>
    </template>
  </WorkspaceListDetail>
</template>
