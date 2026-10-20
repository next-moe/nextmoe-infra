<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'
import { MEDIUM_LABELS } from '~/constants/catalog'
import { WORK_RELATION_PHRASES } from '~/constants/related'
import type { RelatedRow } from '~/utils/relatedRows'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const FIELD = 'catalog.work.relations'

const nav = useWorkspaceNav()
const { entityId, refs, editor } = useEditorContext()
const related = useWorkRelated(entityId)
const relations = useRelatedField(FIELD)

const query = ref('')
const adding = ref(false)

const merged = computed(() =>
  mergeRelatedRows({
    fieldKey: FIELD,
    human: relations.list.value,
    base: relations.baseList.value,
    context: relations.contextRows.value,
    suppressed: relations.suppressed.value
  })
)

const workName = computed(() => {
  const name = editor.draft.value['catalog.work.display_name']
  return typeof name === 'string' && name ? name : '本作'
})

const entries = computed(() =>
  merged.value.map((row) => {
    const workId = String(row.value.work_id ?? '')
    const card = related.display.value.works[workId]
    return {
      row,
      workId,
      known: card?.name || refs.nameOf('work', workId),
      name: card?.name || refs.nameOf('work', workId) || `作品 #${workId}`,
      cover: card?.cover ?? null,
      medium: card?.medium ?? '',
      releaseDate: card?.releaseDate ?? null,
      phrase: relationPhrase(
        String(row.value.relation ?? ''),
        row.value.reverse === true
      ),
      note: String(row.value.note ?? '')
    }
  })
)
type Entry = (typeof entries.value)[number]

const filtered = computed(() =>
  entries.value.filter((entry) =>
    matchesFilter(query.value, entry.name, entry.phrase, entry.workId)
  )
)
const visible = computed(() =>
  filtered.value.filter((entry) => entry.row.state !== 'hidden')
)
const hidden = computed(() =>
  filtered.value.filter((entry) => entry.row.state === 'hidden')
)
const groups = computed(() => {
  const order = Object.keys(WORK_RELATION_PHRASES)
  const byPhrase = new Map<string, Entry[]>()
  for (const entry of visible.value) {
    byPhrase.set(entry.phrase, [...(byPhrase.get(entry.phrase) ?? []), entry])
  }
  const rank = (entry: Entry | undefined) =>
    entry
      ? order.indexOf(String(entry.row.value.relation)) * 2 +
        (entry.row.value.reverse ? 1 : 0)
      : 0
  return [...byPhrase.entries()]
    .sort(([, a], [, b]) => rank(a[0]) - rank(b[0]))
    .map(([phrase, items]) => ({ phrase, items }))
})

const selected = computed(
  () => entries.value.find((entry) => entry.row.key === nav.item.value) ?? null
)
const select = (entry: Entry | null) => {
  nav.item.value = entry?.row.key ?? null
}

const directionOptions = computed(() =>
  relations.memberOptions('relation').flatMap((option) => {
    const phrases = WORK_RELATION_PHRASES[option.token]
    if (!phrases) {
      return [{ value: `${option.token}|0`, label: option.label }]
    }
    return phrases.symmetric
      ? [{ value: `${option.token}|0`, label: phrases.forward }]
      : [
          { value: `${option.token}|0`, label: phrases.forward },
          { value: `${option.token}|1`, label: phrases.reverse }
        ]
  })
)
const directionOf = (row: RelatedRow) => {
  const relation = String(row.value.relation ?? '')
  const symmetric = WORK_RELATION_PHRASES[relation]?.symmetric
  return `${relation}|${row.value.reverse && !symmetric ? 1 : 0}`
}
const splitDirection = (value: string) => {
  const [relation, reverse] = value.split('|')
  return { relation: relation ?? '', reverse: reverse === '1' }
}

const direction = computed<string | null>({
  get: () => (selected.value ? directionOf(selected.value.row) : null),
  set: (next) => {
    if (next && selected.value?.row.origin === 'human') {
      relations.setAt(selected.value.row.index, splitDirection(next))
    }
  }
})
const note = computed({
  get: () => selected.value?.note ?? '',
  set: (next: string | number) => {
    if (selected.value?.row.origin === 'human') {
      relations.setAt(selected.value.row.index, { note: String(next) })
    }
  }
})

const toggle = (entry: Entry) => {
  if (entry.row.origin === 'human') {
    relations.removeAt(entry.row.index)
    select(null)
  } else if (entry.row.identity) {
    relations.toggleSuppressed(entry.row.identity)
  }
}

const draft = reactive<{ work: string | null; direction: string | null }>({
  work: null,
  direction: null
})
const draftTaken = computed(() =>
  draft.work
    ? entries.value.some(
        (entry) =>
          entry.workId === draft.work &&
          directionOf(entry.row) === draft.direction
      )
    : false
)
const addRelation = () => {
  if (!draft.work || !draft.direction || draftTaken.value) {
    return
  }
  const index = relations.append({
    work_id: Number(draft.work),
    ...splitDirection(draft.direction),
    note: ''
  })
  related.ensureWorks([draft.work])
  nav.item.value = humanRowKey(index)
  draft.work = null
  adding.value = false
}

watch(
  () => entries.value.map((entry) => entry.workId).join(','),
  (joined) => {
    const ids = joined ? joined.split(',') : []
    refs.resolve('work', ids)
    if (related.display.value.loaded) {
      related.ensureWorks(ids)
    }
  },
  { immediate: true }
)
watch(
  () => related.display.value.loaded,
  (loaded) => {
    if (loaded) {
      related.ensureWorks(entries.value.map((entry) => entry.workId))
    }
  }
)

onMounted(() => related.load())
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="关联作品列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        :total="visible.length"
        placeholder="按作品名或关系筛选"
        add-label="添加关联"
        :can-add="!relations.disabled.value"
        :adding="adding"
        @add="adding = !adding"
      />
    </template>

    <template #list>
      <WorkspaceRelatedAddPanel
        v-if="adding"
        title="添加关联作品"
        hint="关系从本作出发描述，例如「本作 是…的续作 对方」。同系列、联动这类对称关系不分方向。"
      >
        <EditRefPicker
          v-model="draft.work"
          family="work"
          :exclude="[String(entityId)]"
          aria-label="搜索作品"
          placeholder="搜索作品名或输入 ID"
        />
        <KunSelect
          v-model="draft.direction"
          :options="directionOptions"
          placeholder="选择关系"
          aria-label="关系"
          size="sm"
        />
        <p v-if="draftTaken" class="text-danger-600 text-xs">
          已经有这条关系了。
        </p>
        <KunButton
          size="sm"
          color="primary"
          :disabled="!draft.work || !draft.direction || draftTaken"
          @click="addRelation"
        >
          添加
        </KunButton>
      </WorkspaceRelatedAddPanel>

      <WorkspaceSkeletonList
        v-if="!related.display.value.loaded && !entries.length"
        :rows="4"
      />
      <WorkspaceEmpty
        v-else-if="!entries.length"
        icon="lucide:git-fork"
        title="还没有关联作品"
        description="续作、外传、FD、改编等关系都可以在这里记录。"
      />
      <WorkspaceEmpty
        v-else-if="!filtered.length"
        icon="lucide:search-x"
        title="没有匹配的关联作品"
      />

      <div v-for="group in groups" :key="group.phrase">
        <WorkspaceRelatedGroup
          :label="group.phrase"
          :count="group.items.length"
        />
        <WorkspaceRelatedRow
          v-for="entry in group.items"
          :key="entry.row.key"
          :title="entry.name"
          :selected="selected?.row.key === entry.row.key"
          :state="entry.row.state"
          :picture="entry.cover"
          icon="lucide:gamepad-2"
          shape="cover"
          :source="entry.row.origin === 'upstream' ? entry.row.source : ''"
          :human="entry.row.origin === 'human'"
          @select="select(entry)"
        >
          <template #meta>
            <span v-if="entry.medium" class="shrink-0">
              {{ MEDIUM_LABELS[entry.medium] ?? entry.medium }}
            </span>
            <span v-if="entry.releaseDate" class="text-default-400 shrink-0">
              {{ entry.releaseDate }}
            </span>
            <span v-if="entry.note" class="text-default-400 truncate">
              {{ entry.note }}
            </span>
          </template>
        </WorkspaceRelatedRow>
      </div>

      <WorkspaceRelatedHiddenGroup :count="hidden.length" label="已隐藏的关联">
        <WorkspaceRelatedRow
          v-for="entry in hidden"
          :key="entry.row.key"
          :title="entry.name"
          :subtitle="entry.phrase"
          :selected="selected?.row.key === entry.row.key"
          :state="entry.row.state"
          :picture="entry.cover"
          icon="lucide:gamepad-2"
          shape="cover"
          :source="entry.row.source"
          @select="select(entry)"
        />
      </WorkspaceRelatedHiddenGroup>
    </template>

    <template #detail>
      <div v-if="selected" class="flex h-full min-h-0 flex-col">
        <header
          class="border-default-200 flex shrink-0 items-center gap-3 border-b px-5 py-3"
        >
          <WorkspaceRelatedAvatar
            :picture="selected.cover"
            icon="lucide:gamepad-2"
            shape="cover"
          />
          <div class="min-w-0 flex-1">
            <p class="truncate font-semibold">{{ selected.name }}</p>
            <p class="text-default-500 truncate text-xs">
              {{ MEDIUM_LABELS[selected.medium] ?? '作品' }} · #{{
                selected.workId
              }}
            </p>
          </div>
          <KunButton
            :href="`/works/${selected.workId}`"
            target="_blank"
            variant="light"
            size="sm"
          >
            <KunIcon name="lucide:eye" class="size-4" />
            查看
          </KunButton>
          <KunButton
            :href="`/works/${selected.workId}/edit`"
            target="_blank"
            variant="flat"
            size="sm"
          >
            <KunIcon name="lucide:external-link" class="size-4" />
            编辑该作品
          </KunButton>
        </header>

        <div class="min-h-0 flex-1 space-y-6 overflow-y-auto px-5 py-4">
          <WorkspaceRelatedContextCard
            title="关系"
            :origin="selected.row.origin"
            :source="selected.row.source"
            :state="selected.row.state"
            :note="
              selected.row.origin === 'upstream'
                ? '资料源导入的关系不能直接修改。关系有误时可以隐藏，再添加一条正确的。'
                : ''
            "
          >
            <template #actions>
              <KunButton
                size="xs"
                variant="light"
                :color="selected.row.state === 'hidden' ? 'primary' : 'danger'"
                :disabled="relations.disabled.value"
                @click="toggle(selected)"
              >
                {{
                  selected.row.origin === 'human'
                    ? '删除这条关系'
                    : selected.row.state === 'hidden'
                      ? '恢复显示'
                      : '隐藏'
                }}
              </KunButton>
            </template>

            <p
              class="bg-default-50 border-default-200 rounded-lg border px-3 py-2.5 text-sm leading-6"
            >
              <span class="font-medium">{{ workName }}</span>
              <span class="text-primary-600 mx-1.5 font-semibold">
                {{ selected.phrase }}
              </span>
              <span class="font-medium">{{ selected.name }}</span>
            </p>

            <div
              v-if="selected.row.origin === 'human'"
              class="grid gap-4 sm:grid-cols-2"
            >
              <WorkspaceRelatedAttr label="关系">
                <KunSelect
                  v-model="direction"
                  :options="directionOptions"
                  aria-label="关系"
                  size="sm"
                  :disabled="relations.disabled.value"
                />
              </WorkspaceRelatedAttr>
              <WorkspaceRelatedAttr label="备注">
                <KunInput
                  v-model="note"
                  size="sm"
                  placeholder="可选"
                  aria-label="备注"
                  :disabled="relations.disabled.value"
                />
              </WorkspaceRelatedAttr>
            </div>
            <p v-else-if="selected.note" class="text-default-500 text-sm">
              备注：{{ selected.note }}
            </p>
          </WorkspaceRelatedContextCard>

          <section class="space-y-3">
            <h3 class="text-sm font-semibold">对方作品</h3>
            <div class="border-default-200 flex gap-4 rounded-xl border p-4">
              <WorkspaceRelatedAvatar
                :picture="selected.cover"
                icon="lucide:gamepad-2"
                shape="cover"
                size="lg"
              />
              <div class="min-w-0 space-y-1.5">
                <p class="leading-6 font-semibold">{{ selected.name }}</p>
                <p class="text-default-500 text-sm">
                  {{ MEDIUM_LABELS[selected.medium] ?? '作品' }}
                  <template v-if="selected.releaseDate">
                    · {{ selected.releaseDate }}
                  </template>
                </p>
                <p class="text-default-400 text-xs">
                  对方作品的资料需要在它自己的编辑页修改；修改关系不会改动对方作品的其他信息。
                </p>
              </div>
            </div>
          </section>
        </div>
      </div>
    </template>
  </WorkspaceListDetail>
</template>
