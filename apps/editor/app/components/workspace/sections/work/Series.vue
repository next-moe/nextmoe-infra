<script setup lang="ts">
import type { WorkspaceSectionDef } from '~/constants/workspace'
import { SERIES_MEMBER_KIND_LABELS } from '~/constants/related'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const IDS = 'catalog.work.series_ids'
const MEMBERS = 'catalog.work.series_members'

type Member = { series_id: number; position: number; kind: number }

const nav = useWorkspaceNav()
const { entityId, refs, editor } = useEditorContext()
const related = useWorkRelated(entityId)
const series = useRelatedField(IDS)
const members = useRelatedField(MEMBERS)

const query = ref('')
const adding = ref(false)
const pick = ref<string | null>(null)

const memberIndex = (seriesId: string) =>
  (members.list.value as Member[]).findIndex(
    (row) => String(row.series_id) === seriesId
  )
const baseMember = (seriesId: string) =>
  (members.baseList.value as Member[]).find(
    (row) => String(row.series_id) === seriesId
  )

const rows = computed(() =>
  mergeIdRows({
    human: series.list.value,
    base: series.baseList.value,
    context: series.contextRows.value,
    suppressed: series.suppressed.value
  }).map((row) => {
    const info = related.display.value.series[row.id]
    const index = memberIndex(row.id)
    const member =
      index >= 0 ? (members.list.value as Member[])[index] : undefined
    const prior = baseMember(row.id)
    const moved =
      member && prior
        ? member.position !== prior.position || member.kind !== prior.kind
        : false
    return {
      ...row,
      state: row.state ?? (moved ? ('changed' as const) : null),
      known: info?.name || refs.nameOf('series', row.id),
      name: info?.name || refs.nameOf('series', row.id) || `系列 #${row.id}`,
      curated: info ? info.source === 'curated' : null,
      memberCount: info?.memberCount ?? null,
      memberIndex: index,
      member
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

const selected = computed(
  () => rows.value.find((row) => row.key === nav.item.value) ?? null
)
const select = (row: Row | null) => {
  nav.item.value = row?.key ?? null
}

const kindOptions = computed(() =>
  members.memberOptions('kind', SERIES_MEMBER_KIND_LABELS)
)
const kindLabel = (wire: unknown) => {
  const token = members.tokenOf('kind', wire)
  return token ? (SERIES_MEMBER_KIND_LABELS[token] ?? token) : ''
}

const setMember = (row: Row, patch: Partial<Member>) => {
  if (row.memberIndex >= 0) {
    members.setAt(row.memberIndex, patch)
  }
}

const removeRow = (row: Row) => {
  if (row.origin === 'human') {
    series.removeAt(row.index)
    select(null)
  } else if (row.identity) {
    series.toggleSuppressed(row.identity)
  }
}

watch(pick, (id) => {
  if (!id) {
    return
  }
  pick.value = null
  adding.value = false
  const existing = rows.value.find((row) => row.id === id)
  if (existing) {
    select(existing)
    return
  }
  const index = series.append(Number(id))
  nav.item.value = humanRowKey(index)
})

watch(
  () => rows.value.map((row) => row.id).join(','),
  (joined) => refs.resolve('series', joined ? joined.split(',') : []),
  { immediate: true }
)

onMounted(() => related.load())
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="系列列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        :total="visible.length"
        placeholder="按系列名筛选"
        add-label="加入系列"
        :can-add="editor.fieldByKey.value.has(IDS) && !series.disabled.value"
        :adding="adding"
        @add="adding = !adding"
      />
    </template>

    <template #list>
      <WorkspaceRelatedAddPanel
        v-if="adding"
        title="把这部作品加入一个系列"
        hint="只能加入人工维护的系列；资料源生成的系列由导入流程管理。没有合适的系列时可以先新建。"
      >
        <EditRefPicker
          v-model="pick"
          family="series"
          :exclude="rows.map((row) => row.id)"
          aria-label="搜索系列"
          placeholder="搜索系列名或输入 ID"
        />
        <KunButton href="/new/series" target="_blank" size="xs" variant="light">
          <KunIcon name="lucide:external-link" class="size-3.5" />
          新建系列
        </KunButton>
      </WorkspaceRelatedAddPanel>

      <WorkspaceEmpty
        v-if="!rows.length"
        icon="lucide:library"
        title="这部作品还不属于任何系列"
        description="用右上角的「加入系列」把它放进一个系列，再设置它在系列中的位置。"
      />
      <WorkspaceEmpty
        v-else-if="!filtered.length"
        icon="lucide:search-x"
        title="没有匹配的系列"
      />

      <WorkspaceRelatedRow
        v-for="row in visible"
        :key="row.key"
        :title="row.name"
        :selected="selected?.key === row.key"
        :state="row.state"
        icon="lucide:library"
        :source="row.origin === 'upstream' ? row.source : ''"
        :human="row.origin === 'human'"
        @select="select(row)"
      >
        <template #meta>
          <span v-if="row.member" class="shrink-0">
            第 {{ row.member.position }} 部 · {{ kindLabel(row.member.kind) }}
          </span>
          <span v-if="row.memberCount !== null" class="text-default-400">
            共 {{ row.memberCount }} 部
          </span>
          <span v-if="!row.member && !row.memberCount" class="text-default-400">
            #{{ row.id }}
          </span>
        </template>
      </WorkspaceRelatedRow>

      <WorkspaceRelatedHiddenGroup :count="hidden.length" label="已隐藏的系列">
        <WorkspaceRelatedRow
          v-for="row in hidden"
          :key="row.key"
          :title="row.name"
          :selected="selected?.key === row.key"
          :state="row.state"
          icon="lucide:library"
          :source="row.source"
          @select="select(row)"
        />
      </WorkspaceRelatedHiddenGroup>
    </template>

    <template #detail>
      <WorkspaceEntityPanel
        v-if="selected"
        :key="selected.id"
        family="series"
        :entity-id="selected.id"
        :label="selected.known"
        :subtitle="
          selected.memberCount !== null
            ? `系列 · 共 ${selected.memberCount} 部作品`
            : `系列 · #${selected.id}`
        "
      >
        <template #context>
          <WorkspaceRelatedContextCard
            title="在系列中的位置"
            :origin="selected.origin"
            :source="selected.source"
            :state="selected.state"
          >
            <template #actions>
              <KunButton
                size="xs"
                variant="light"
                :color="selected.state === 'hidden' ? 'primary' : 'danger'"
                :disabled="series.disabled.value"
                @click="removeRow(selected)"
              >
                {{
                  selected.origin === 'human'
                    ? '退出这个系列'
                    : selected.state === 'hidden'
                      ? '恢复显示'
                      : '在本作中隐藏'
                }}
              </KunButton>
            </template>
            <div
              v-if="selected.member"
              class="grid gap-4 sm:grid-cols-[10rem_minmax(0,1fr)]"
            >
              <WorkspaceRelatedAttr
                label="序号"
                hint="这部作品在系列中的排列位置。"
              >
                <KunNumberInput
                  :model-value="selected.member.position"
                  :min="0"
                  size="sm"
                  aria-label="系列内序号"
                  :disabled="members.disabled.value"
                  @update:model-value="
                    setMember(selected, { position: Number($event ?? 0) })
                  "
                />
              </WorkspaceRelatedAttr>
              <WorkspaceRelatedAttr label="身份">
                <WorkspaceRelatedChoice
                  :model-value="selected.member.kind"
                  :options="kindOptions"
                  label="在系列中的身份"
                  :disabled="members.disabled.value"
                  @update:model-value="
                    setMember(selected, { kind: Number($event) })
                  "
                />
              </WorkspaceRelatedAttr>
            </div>
            <p v-else class="text-default-500 text-sm">
              {{
                selected.state === 'added'
                  ? '加入系列的修改生效后，就可以在这里设置序号和身份。'
                  : '这个系列没有记录这部作品的位置。'
              }}
            </p>
          </WorkspaceRelatedContextCard>
        </template>
      </WorkspaceEntityPanel>
    </template>
  </WorkspaceListDetail>
</template>
