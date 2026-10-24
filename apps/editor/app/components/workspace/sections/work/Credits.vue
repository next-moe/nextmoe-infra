<script setup lang="ts">
import type { EditRefFamily } from '#shared/types/edit'
import type { WorkspaceSectionDef } from '~/constants/workspace'
import type { RelatedRow, RelatedValue } from '~/utils/relatedRows'
import { rowIdentity } from '#shared/utils/editModel'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const FIELD = 'catalog.work.credits'
const UPSTREAM_FIELD = 'catalog.work.credits_upstream'

const nav = useWorkspaceNav()
const { entityId, refs } = useEditorContext()
const related = useWorkRelated(entityId)
const credits = useRelatedField(FIELD)
const upstreamCredits = useRelatedField(UPSTREAM_FIELD)

const query = ref('')
const adding = ref(false)
const roleOrder = ref<string[]>([])

const creditKey = (row: unknown) => rowIdentity(UPSTREAM_FIELD, row)

const upstreamDraft = computed(
  () =>
    new Map(
      upstreamCredits.list.value.map((row, index) => [
        creditKey(row),
        { row: row as RelatedValue, index }
      ])
    )
)
const upstreamBase = computed(
  () =>
    new Map(
      upstreamCredits.baseList.value.map((row) => [
        creditKey(row),
        row as RelatedValue
      ])
    )
)

const merged = computed(() =>
  mergeRelatedRows({
    fieldKey: FIELD,
    human: credits.list.value,
    base: credits.baseList.value,
    context: credits.contextRows.value,
    suppressed: credits.suppressed.value
  }).map((row) => {
    const draft =
      row.origin === 'upstream'
        ? upstreamDraft.value.get(creditKey(row.value))
        : null
    if (!draft) {
      return row
    }
    const base = upstreamBase.value.get(creditKey(row.value))
    const changed = base ? !sameCreditAttrs(base, draft.row) : false
    return {
      ...row,
      value: { ...row.value, ...creditAttrsOf(draft.row) },
      index: draft.index,
      state: row.state ?? (changed ? ('changed' as const) : null)
    }
  })
)

const nameOf = (family: EditRefFamily, id: unknown, fallback: string) =>
  refs.nameOf(family, id) ||
  (id == null || id === '' ? '' : `${fallback} #${id}`)

const describe = (row: RelatedRow) => {
  const value = row.value
  const characterId = value.character_id ? String(value.character_id) : ''
  return {
    row,
    roleId: String(value.role_id ?? ''),
    roleName: nameOf('role', value.role_id, '职位'),
    creditNameId: String(value.credit_name_id ?? ''),
    name: nameOf('credit_name', value.credit_name_id, '署名'),
    characterId,
    characterName: characterId
      ? refs.nameOf('character', characterId) ||
        related.display.value.characters[characterId]?.name ||
        nameOf('character', characterId, '角色')
      : '',
    note: String(value.note ?? ''),
    picture: characterId
      ? (related.display.value.characters[characterId]?.image ?? null)
      : null,
    linked: Boolean(
      related.display.value.creditPersons[String(value.credit_name_id ?? '')]
    )
  }
}

const entries = computed(() => merged.value.map(describe))
type Entry = (typeof entries.value)[number]

const filtered = computed(() =>
  entries.value.filter((entry) =>
    matchesFilter(
      query.value,
      entry.name,
      entry.roleName,
      entry.characterName,
      entry.note
    )
  )
)
const visible = computed(() =>
  filtered.value.filter((entry) => entry.row.state !== 'hidden')
)
const hidden = computed(() =>
  filtered.value.filter((entry) => entry.row.state === 'hidden')
)
const groups = computed(() => {
  const byRole = new Map<string, Entry[]>()
  for (const entry of visible.value) {
    byRole.set(entry.roleId, [...(byRole.get(entry.roleId) ?? []), entry])
  }
  const rank = (id: string) => {
    const at = roleOrder.value.indexOf(id)
    return at < 0 ? Number.MAX_SAFE_INTEGER : at
  }
  return [...byRole.entries()]
    .sort(([a], [b]) => rank(a) - rank(b))
    .map(([roleId, items]) => ({
      roleId,
      label: items[0]?.roleName ?? '',
      items
    }))
})

const selected = computed(
  () => entries.value.find((entry) => entry.row.key === nav.item.value) ?? null
)
const select = (entry: Entry | null) => {
  nav.item.value = entry?.row.key ?? null
}
useAutoSelectFirst(selected, () => groups.value[0]?.items[0], select)

const panel = ref<'credit_name' | 'person'>('credit_name')
const personId = computed(() =>
  selected.value
    ? (related.display.value.creditPersons[selected.value.creditNameId] ?? null)
    : null
)
const panelFamily = computed(() =>
  panel.value === 'person' && personId.value ? 'person' : 'credit_name'
)
const panelId = computed(() =>
  panelFamily.value === 'person'
    ? personId.value!
    : (selected.value?.creditNameId ?? '')
)

const upstreamRows = computed(() =>
  merged.value.filter((row) => row.origin === 'upstream')
)
const clashes = computed(() =>
  selected.value?.row.origin === 'human'
    ? creditKeyClash(selected.value.row.value, upstreamRows.value)
    : false
)

const setMember = (key: string, next: unknown) => {
  const row = selected.value?.row
  if (!row || row.index < 0) {
    return
  }
  const target = row.origin === 'human' ? credits : upstreamCredits
  target.setAt(row.index, { [key]: next === '' ? null : next })
}

const attrsDisabled = computed(() => {
  const row = selected.value?.row
  if (!row) {
    return true
  }
  return row.origin === 'human'
    ? credits.disabled.value
    : upstreamCredits.disabled.value || row.index < 0 || row.state === 'hidden'
})

const undoCorrection = () => {
  const row = selected.value?.row
  const base = row ? upstreamBase.value.get(creditKey(row.value)) : undefined
  if (row?.origin === 'upstream' && row.index >= 0 && base) {
    upstreamCredits.setAt(row.index, creditAttrsOf(base))
  }
}

const refModel = (key: string) =>
  computed<string | null>({
    get: () => {
      const value = selected.value?.row.value[key]
      return value === null || value === undefined || value === ''
        ? null
        : String(value)
    },
    set: (next) => setMember(key, next === null ? null : Number(next))
  })
const roleModel = refModel('role_id')
const creditNameModel = refModel('credit_name_id')
const characterModel = refModel('character_id')
const releaseModel = refModel('release_id')
const labelModel = refModel('label_id')
const noteModel = computed({
  get: () => String(selected.value?.row.value.note ?? ''),
  set: (next: string | number) => setMember('note', String(next))
})

const spoilerOptions = computed(() => credits.memberOptions('spoiler'))

const remove = () => {
  if (selected.value?.row.origin === 'human') {
    credits.removeAt(selected.value.row.index)
    select(null)
  }
}

const draft = reactive<{
  role: string | null
  creditName: string | null
  character: string | null
}>({ role: null, creditName: null, character: null })

const draftClash = computed(() =>
  draft.role && draft.creditName
    ? creditKeyClash(
        {
          role_id: draft.role,
          credit_name_id: draft.creditName,
          character_id: draft.character
        },
        upstreamRows.value
      )
    : false
)

const addCredit = () => {
  if (!draft.role || !draft.creditName || draftClash.value) {
    return
  }
  const row: RelatedValue = {
    role_id: Number(draft.role),
    credit_name_id: Number(draft.creditName),
    character_id: draft.character ? Number(draft.character) : null,
    spoiler: credits.wireBase('spoiler')
  }
  const index = credits.append(row)
  draft.creditName = null
  draft.character = null
  adding.value = false
  nav.item.value = humanRowKey(index)
}

const identityAttrs = computed(() =>
  selected.value
    ? [
        { label: '职位', text: selected.value.roleName },
        { label: '署名名', text: selected.value.name },
        { label: '配音角色', text: selected.value.characterName }
      ]
    : []
)

const idsOf = (key: string) =>
  [
    ...new Set(
      merged.value
        .map((row) => row.value[key])
        .filter((id) => id != null && id !== '')
        .map(String)
    )
  ].join(',')

watch(
  [() => idsOf('credit_name_id'), () => idsOf('character_id')],
  ([names, characters]) => {
    const split = (joined: string) => (joined ? joined.split(',') : [])
    related.ensureCreditPersons(split(names))
    refs.resolve('character', split(characters))
  },
  { immediate: true }
)

watch(
  () => selected.value?.row.key ?? '',
  () => {
    const entry = selected.value
    if (!entry) {
      return
    }
    refs.resolve('release', [String(entry.row.value.release_id ?? '')])
    refs.resolve('company', [String(entry.row.value.label_id ?? '')])
    if (!personId.value) {
      panel.value = 'credit_name'
    }
  },
  { immediate: true }
)

onMounted(async () => {
  related.load()
  const roles = await refs.loadRoles()
  roleOrder.value = roles.map((role) => role.value)
})
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="制作人员列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        :total="visible.length"
        placeholder="按名字、职位或角色筛选"
        add-label="添加署名"
        :can-add="!credits.disabled.value"
        :adding="adding"
        @add="adding = !adding"
      />
    </template>

    <template #list>
      <WorkspaceRelatedAddPanel
        v-if="adding"
        title="添加一条署名"
        hint="署名名是作品职员表里出现的名字；找不到时先新建署名名。声优署名请同时选择配音的角色。"
      >
        <EditRefPicker
          v-model="draft.role"
          family="role"
          aria-label="职位"
          placeholder="选择职位"
        />
        <EditRefPicker
          v-model="draft.creditName"
          family="credit_name"
          aria-label="署名名"
          placeholder="搜索署名名或输入 ID"
        />
        <EditRefPicker
          v-model="draft.character"
          family="character"
          aria-label="配音角色"
          placeholder="配音角色（可选）"
        />
        <p v-if="draftClash" class="text-danger-600 text-xs">
          资料源里已经有这条署名，不能重复添加。
        </p>
        <div class="flex items-center gap-2">
          <KunButton
            size="sm"
            color="primary"
            :disabled="!draft.role || !draft.creditName || draftClash"
            @click="addCredit"
          >
            添加
          </KunButton>
          <KunButton
            href="/new/credit_name"
            target="_blank"
            size="sm"
            variant="light"
          >
            <KunIcon name="lucide:external-link" class="size-3.5" />
            新建署名名
          </KunButton>
        </div>
      </WorkspaceRelatedAddPanel>

      <WorkspaceSkeletonList
        v-if="!related.display.value.loaded && !entries.length"
      />
      <WorkspaceEmpty
        v-else-if="!entries.length"
        icon="lucide:clapperboard"
        title="还没有制作人员"
        description="用右上角的「添加署名」录入原画、剧本、声优等。"
      />
      <WorkspaceEmpty
        v-else-if="!filtered.length"
        icon="lucide:search-x"
        title="没有匹配的署名"
      />

      <div v-for="group in groups" :key="group.roleId">
        <WorkspaceRelatedGroup
          :label="group.label"
          :count="group.items.length"
        />
        <WorkspaceRelatedRow
          v-for="entry in group.items"
          :key="entry.row.key"
          :title="entry.name"
          :selected="selected?.row.key === entry.row.key"
          :state="entry.row.state"
          :picture="entry.picture"
          :icon="entry.characterId ? 'lucide:user-round' : 'lucide:signature'"
          :shape="entry.characterId ? 'circle' : 'square'"
          :source="entry.row.origin === 'upstream' ? entry.row.source : ''"
          :human="entry.row.origin === 'human'"
          @select="select(entry)"
        >
          <template v-if="entry.characterName || entry.note" #meta>
            <span v-if="entry.characterName" class="truncate">
              {{ entry.characterName }}
            </span>
            <span
              v-if="entry.note"
              class="text-default-400 truncate"
              :title="entry.note"
            >
              {{ entry.note }}
            </span>
          </template>
          <template #trailing>
            <KunTooltip v-if="entry.linked" text="已归属人物" position="left">
              <KunIcon
                name="lucide:contact"
                class="text-default-400 size-3.5"
              />
            </KunTooltip>
          </template>
        </WorkspaceRelatedRow>
      </div>

      <WorkspaceRelatedHiddenGroup :count="hidden.length" label="已隐藏的署名">
        <WorkspaceRelatedRow
          v-for="entry in hidden"
          :key="entry.row.key"
          :title="entry.name"
          :subtitle="
            [entry.roleName, entry.characterName].filter(Boolean).join(' · ')
          "
          :selected="selected?.row.key === entry.row.key"
          :state="entry.row.state"
          icon="lucide:signature"
          :source="entry.row.source"
          @select="select(entry)"
        />
      </WorkspaceRelatedHiddenGroup>
    </template>

    <template #detail>
      <WorkspaceEntityPanel
        v-if="selected && panelId"
        :key="`${panelFamily}:${panelId}`"
        :family="panelFamily"
        :entity-id="panelId"
        :label="
          panelFamily === 'credit_name'
            ? refs.nameOf('credit_name', selected.creditNameId)
            : ''
        "
        :subtitle="
          panelFamily === 'credit_name'
            ? `署名名 · ${selected.roleName}`
            : `人物 · 署名「${selected.name}」`
        "
      >
        <template #context>
          <div class="space-y-4">
            <WorkspaceRelatedContextCard
              :origin="selected.row.origin"
              :source="selected.row.source"
              :state="selected.row.state"
            >
              <template #actions>
                <template v-if="selected.row.origin === 'upstream'">
                  <KunButton
                    v-if="selected.row.state === 'changed'"
                    size="xs"
                    variant="light"
                    :disabled="upstreamCredits.disabled.value"
                    @click="undoCorrection"
                  >
                    <KunIcon name="lucide:undo-2" class="size-3.5" />
                    撤销修改
                  </KunButton>
                  <KunButton
                    size="xs"
                    variant="light"
                    :color="
                      selected.row.state === 'hidden' ? 'primary' : 'danger'
                    "
                    :disabled="credits.disabled.value"
                    @click="credits.toggleSuppressed(selected.row.identity!)"
                  >
                    <KunIcon
                      :name="
                        selected.row.state === 'hidden'
                          ? 'lucide:eye'
                          : 'lucide:eye-off'
                      "
                      class="size-3.5"
                    />
                    {{ selected.row.state === 'hidden' ? '恢复显示' : '隐藏' }}
                  </KunButton>
                </template>
                <KunButton
                  v-else
                  size="xs"
                  variant="light"
                  color="danger"
                  :disabled="credits.disabled.value"
                  @click="remove"
                >
                  <KunIcon name="lucide:trash-2" class="size-3.5" />
                  删除这条署名
                </KunButton>
              </template>

              <div class="grid gap-4 sm:grid-cols-2">
                <template v-if="selected.row.origin === 'human'">
                  <WorkspaceRelatedAttr label="职位">
                    <EditRefPicker
                      v-model="roleModel"
                      family="role"
                      aria-label="职位"
                      :disabled="credits.disabled.value"
                    />
                  </WorkspaceRelatedAttr>
                  <WorkspaceRelatedAttr label="署名名">
                    <EditRefPicker
                      v-model="creditNameModel"
                      family="credit_name"
                      aria-label="署名名"
                      :disabled="credits.disabled.value"
                    />
                  </WorkspaceRelatedAttr>
                  <WorkspaceRelatedAttr
                    label="配音角色"
                    hint="声优署名填写所配的角色，其他职位留空。"
                  >
                    <EditRefPicker
                      v-model="characterModel"
                      family="character"
                      aria-label="配音角色"
                      :disabled="credits.disabled.value"
                    />
                  </WorkspaceRelatedAttr>
                </template>
                <template v-else>
                  <WorkspaceRelatedAttr
                    v-for="attr in identityAttrs"
                    :key="attr.label"
                    :label="attr.label"
                  >
                    <p
                      class="border-default-200 bg-default-50 text-default-700 flex h-8 items-center truncate rounded-lg border px-3 text-sm"
                    >
                      {{ attr.text || '—' }}
                    </p>
                  </WorkspaceRelatedAttr>
                </template>
                <WorkspaceRelatedAttr label="备注">
                  <KunInput
                    v-model="noteModel"
                    size="sm"
                    placeholder="可选，例如「仅限 FD」"
                    aria-label="备注"
                    :disabled="attrsDisabled"
                  />
                </WorkspaceRelatedAttr>
                <WorkspaceRelatedAttr label="剧透等级" wide>
                  <WorkspaceRelatedChoice
                    :model-value="
                      (selected.row.value.spoiler as number) ?? null
                    "
                    :options="spoilerOptions"
                    label="剧透等级"
                    :disabled="attrsDisabled"
                    @update:model-value="setMember('spoiler', $event)"
                  />
                </WorkspaceRelatedAttr>
                <WorkspaceRelatedAttr
                  label="仅限发行"
                  hint="只在某个发行版本中担任这个职位时填写。"
                >
                  <EditRefPicker
                    v-model="releaseModel"
                    family="release"
                    aria-label="仅限发行"
                    :disabled="attrsDisabled"
                  />
                </WorkspaceRelatedAttr>
                <WorkspaceRelatedAttr label="所属厂商">
                  <EditRefPicker
                    v-model="labelModel"
                    family="company"
                    aria-label="所属厂商"
                    :disabled="attrsDisabled"
                  />
                </WorkspaceRelatedAttr>
                <p v-if="clashes" class="text-danger-600 text-xs sm:col-span-2">
                  资料源里已有职位、署名、角色都相同的记录。请至少修改其中一项，否则提交会被拒绝。
                </p>
                <p
                  v-if="selected.row.origin === 'upstream'"
                  class="text-default-500 text-xs leading-5 sm:col-span-2"
                >
                  {{
                    selected.row.state === 'hidden'
                      ? '这条署名已隐藏，恢复显示后才能修改。'
                      : '备注、剧透、发行、厂商可以直接修改，资料源以后同步也不会覆盖。职位、署名名、配音角色错了的话，请隐藏这条，再添加一条正确的署名。'
                  }}
                </p>
              </div>
            </WorkspaceRelatedContextCard>

            <div
              class="border-default-200 flex items-center gap-2 border-t pt-4"
            >
              <span class="text-default-500 shrink-0 text-xs whitespace-nowrap"
                >切换编辑</span
              >
              <KunRadioGroup
                v-model="panel"
                :options="[
                  { value: 'credit_name', label: '署名名' },
                  {
                    value: 'person',
                    label: '人物',
                    disabled: !personId
                  }
                ]"
                aria-label="编辑署名名或人物"
                variant="pill"
                orientation="horizontal"
                size="sm"
              />
              <span v-if="!personId" class="text-default-400 text-xs">
                这个署名还没有归属人物，可以在下方「归属人物」里指定。
              </span>
            </div>
          </div>
        </template>
      </WorkspaceEntityPanel>
    </template>
  </WorkspaceListDetail>
</template>
