<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'
import { ROSTER_ROLE_LABELS } from '~/constants/catalog'
import { ROSTER_ROLE_ORDER } from '~/constants/related'
import { vocabLabel } from '~/constants/vocabLabels'
import type { RelatedState } from '~/utils/relatedRows'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const FIELD = 'catalog.work.roster'
const CREDITS = 'catalog.work.credits'

type RosterValue = { character_id: number; kind: number; spoiler: number }

const nav = useWorkspaceNav()
const { entityId, refs } = useEditorContext()
const related = useWorkRelated(entityId)
const roster = useRelatedField(FIELD)
const credits = useRelatedField(CREDITS)

const query = ref('')
const adding = ref(false)
const pick = ref<string | null>(null)

const contextByCharacter = computed(
  () =>
    new Map(
      roster.contextRows.value.map((row) => [
        String((row.value as RosterValue | null)?.character_id ?? ''),
        row
      ])
    )
)
const baseByCharacter = computed(
  () =>
    new Map(
      (roster.baseList.value as RosterValue[]).map((row) => [
        String(row.character_id),
        row
      ])
    )
)

const rows = computed(() =>
  (roster.list.value as RosterValue[]).map((value, index) => {
    const id = String(value.character_id)
    const context = contextByCharacter.value.get(id)
    const upstream = Boolean(context && !context.human_lane && context.identity)
    const base = baseByCharacter.value.get(id)
    const hidden = Boolean(
      upstream && roster.suppressed.value.includes(context!.identity!)
    )
    const state: RelatedState = !base
      ? 'added'
      : hidden
        ? 'hidden'
        : base.kind !== value.kind || base.spoiler !== value.spoiler
          ? 'changed'
          : null
    const character = related.display.value.characters[id]
    return {
      id,
      index,
      value,
      identity: upstream ? context!.identity! : null,
      origin: upstream ? ('upstream' as const) : ('human' as const),
      source: upstream ? sourceLabel(context!.source) : '',
      state,
      known: refs.nameOf('character', id) || character?.name,
      name: refs.nameOf('character', id) || character?.name || `角色 #${id}`,
      role: roster.tokenOf('kind', value.kind) ?? 'unknown',
      spoiler: roster.tokenOf('spoiler', value.spoiler) ?? 'none',
      picture: character?.image ?? null
    }
  })
)

type Row = (typeof rows.value)[number]

const filtered = computed(() =>
  rows.value.filter((row) => matchesFilter(query.value, row.name, row.id))
)
const visible = computed(() =>
  filtered.value.filter((row) => row.state !== 'hidden')
)
const hidden = computed(() =>
  filtered.value.filter((row) => row.state === 'hidden')
)
const groups = computed(() =>
  ROSTER_ROLE_ORDER.map((role) => ({
    role,
    label: ROSTER_ROLE_LABELS[role] ?? role,
    items: visible.value.filter((row) => row.role === role)
  })).filter((group) => group.items.length)
)

const selected = computed(
  () => rows.value.find((row) => row.id === nav.item.value) ?? null
)
const select = (row: Row | null) => {
  nav.item.value = row?.id ?? null
}
useAutoSelectFirst(selected, () => groups.value[0]?.items[0], select)

const voicesOf = (characterId: string) => {
  const mergedCredits = mergeRelatedRows({
    fieldKey: CREDITS,
    human: credits.list.value,
    base: credits.baseList.value,
    context: credits.contextRows.value,
    suppressed: credits.suppressed.value
  })
  return mergedCredits
    .filter(
      (row) =>
        row.state !== 'hidden' &&
        String(row.value.character_id ?? '') === characterId
    )
    .map((row) => ({
      key: row.key,
      creditNameId: String(row.value.credit_name_id ?? ''),
      name:
        refs.nameOf('credit_name', row.value.credit_name_id) ||
        `署名 #${row.value.credit_name_id}`
    }))
}
const selectedVoices = computed(() =>
  selected.value ? voicesOf(selected.value.id) : []
)

watch(
  () => selectedVoices.value.map((voice) => voice.creditNameId).join(','),
  (joined) => refs.resolve('credit_name', joined ? joined.split(',') : []),
  { immediate: true }
)

const roleOptions = computed(() => roster.memberOptions('kind'))
const spoilerOptions = computed(() => roster.memberOptions('spoiler'))

const setMember = (row: Row, key: 'kind' | 'spoiler', next: unknown) => {
  roster.setAt(row.index, { [key]: Number(next) })
}

const remove = (row: Row) => {
  roster.removeAt(row.index)
  select(null)
}

watch(pick, (id) => {
  if (!id) {
    return
  }
  pick.value = null
  adding.value = false
  if (!rows.value.some((row) => row.id === id)) {
    roster.append({
      character_id: Number(id),
      kind: roster.wireBase('kind'),
      spoiler: roster.wireBase('spoiler')
    })
    related.ensureCharacters([id])
  }
  nav.item.value = id
})

watch(
  [
    () =>
      (roster.list.value as RosterValue[])
        .map((row) => row.character_id)
        .join(','),
    () => related.display.value.loaded
  ],
  ([joined, loaded]) => {
    const ids = joined ? joined.split(',') : []
    refs.resolve('character', ids)
    if (loaded) {
      related.ensureCharacters(ids)
    }
  },
  { immediate: true }
)

onMounted(() => related.load())
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="角色列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        :total="visible.length"
        placeholder="按角色名或 ID 筛选"
        add-label="添加角色"
        :can-add="!roster.disabled.value"
        :adding="adding"
        @add="adding = !adding"
      />
    </template>

    <template #list>
      <WorkspaceRelatedAddPanel
        v-if="adding"
        title="把已有角色加入这部作品"
        hint="找不到这个角色？先新建，再回来添加。"
      >
        <EditRefPicker
          v-model="pick"
          family="character"
          :exclude="rows.map((row) => row.id)"
          aria-label="搜索角色"
          placeholder="搜索角色名或输入 ID"
        />
        <KunButton
          href="/new/character"
          target="_blank"
          size="xs"
          variant="light"
        >
          <KunIcon name="lucide:external-link" class="size-3.5" />
          新建角色
        </KunButton>
      </WorkspaceRelatedAddPanel>

      <WorkspaceSkeletonList
        v-if="!related.display.value.loaded && !rows.length"
        avatar
      />
      <WorkspaceEmpty
        v-else-if="!rows.length"
        icon="lucide:users"
        title="还没有登场角色"
        description="用右上角的「添加角色」把已有角色加入这部作品。"
      />
      <WorkspaceEmpty
        v-else-if="!filtered.length"
        icon="lucide:search-x"
        title="没有匹配的角色"
      />

      <div v-for="group in groups" :key="group.role">
        <WorkspaceRelatedGroup
          :label="group.label"
          :count="group.items.length"
        />
        <WorkspaceRelatedRow
          v-for="row in group.items"
          :key="row.id"
          :title="row.name"
          :selected="selected?.id === row.id"
          :state="row.state"
          :picture="row.picture"
          icon="lucide:user-round"
          shape="circle"
          @select="select(row)"
        >
          <template #meta>
            <span
              v-if="row.spoiler !== 'none'"
              class="text-warning-600 shrink-0"
            >
              {{ vocabLabel('spoiler', row.spoiler) }}
            </span>
            <span class="truncate">
              {{
                related.display.value.characters[row.id]?.voices
                  .map(
                    (voice) =>
                      refs.nameOf('credit_name', voice.id) || voice.name
                  )
                  .join('、') || `#${row.id}`
              }}
            </span>
          </template>
        </WorkspaceRelatedRow>
      </div>

      <WorkspaceRelatedHiddenGroup :count="hidden.length" label="已隐藏的角色">
        <WorkspaceRelatedRow
          v-for="row in hidden"
          :key="row.id"
          :title="row.name"
          :selected="selected?.id === row.id"
          :state="row.state"
          :picture="row.picture"
          icon="lucide:user-round"
          shape="circle"
          :subtitle="`#${row.id}`"
          @select="select(row)"
        />
      </WorkspaceRelatedHiddenGroup>
    </template>

    <template #detail>
      <WorkspaceEntityPanel
        v-if="selected"
        :key="selected.id"
        family="character"
        :entity-id="selected.id"
        :label="selected.known"
        :thumbnail="selected.picture"
        :subtitle="`${ROSTER_ROLE_LABELS[selected.role] ?? '角色'} · #${selected.id}`"
      >
        <template #context>
          <WorkspaceRelatedContextCard
            :origin="selected.origin"
            :source="selected.source"
            :state="selected.state"
            :note="
              selected.origin === 'upstream'
                ? '资料源导入的角色不能删除；不属于这部作品时可以隐藏，资料源下次同步也不会再显示。'
                : ''
            "
          >
            <template #actions>
              <KunButton
                v-if="selected.origin === 'upstream' && selected.identity"
                size="xs"
                variant="light"
                :color="selected.state === 'hidden' ? 'primary' : 'danger'"
                :disabled="roster.disabled.value"
                @click="roster.toggleSuppressed(selected.identity)"
              >
                <KunIcon
                  :name="
                    selected.state === 'hidden'
                      ? 'lucide:eye'
                      : 'lucide:eye-off'
                  "
                  class="size-3.5"
                />
                {{ selected.state === 'hidden' ? '恢复显示' : '在本作中隐藏' }}
              </KunButton>
              <KunButton
                v-else
                size="xs"
                variant="light"
                color="danger"
                :disabled="roster.disabled.value"
                @click="remove(selected)"
              >
                <KunIcon name="lucide:trash-2" class="size-3.5" />
                从本作移除
              </KunButton>
            </template>
            <div class="grid gap-4 sm:grid-cols-2">
              <WorkspaceRelatedAttr label="出场程度">
                <WorkspaceRelatedChoice
                  :model-value="selected.value.kind"
                  :options="roleOptions"
                  label="出场程度"
                  :disabled="roster.disabled.value"
                  @update:model-value="setMember(selected, 'kind', $event)"
                />
              </WorkspaceRelatedAttr>
              <WorkspaceRelatedAttr label="剧透等级">
                <WorkspaceRelatedChoice
                  :model-value="selected.value.spoiler"
                  :options="spoilerOptions"
                  label="剧透等级"
                  :disabled="roster.disabled.value"
                  @update:model-value="setMember(selected, 'spoiler', $event)"
                />
              </WorkspaceRelatedAttr>
              <WorkspaceRelatedAttr label="配音" wide>
                <div
                  v-if="selectedVoices.length"
                  class="flex flex-wrap gap-1.5"
                >
                  <button
                    v-for="voice in selectedVoices"
                    :key="voice.key"
                    type="button"
                    class="border-default-200 hover:border-primary-300 hover:bg-primary-50 inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-sm transition-colors"
                    @click="nav.go('credits', voice.key)"
                  >
                    <KunIcon
                      name="lucide:mic"
                      class="text-default-400 size-3.5"
                    />
                    {{ voice.name }}
                  </button>
                </div>
                <p v-else class="text-default-400 text-sm">
                  还没有配音署名，可以在「制作人员」里添加声优。
                </p>
              </WorkspaceRelatedAttr>
            </div>
          </WorkspaceRelatedContextCard>
        </template>
      </WorkspaceEntityPanel>
    </template>
  </WorkspaceListDetail>
</template>
