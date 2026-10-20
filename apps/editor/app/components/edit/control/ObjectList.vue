<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaMember } from '#shared/types/edit'
import type { MergedRow } from '~/utils/mergedRows'

const props = defineProps<{ fieldKey: string }>()

const list = useMergedRows(() => props.fieldKey)
const { config, field, issues, disabled, rows, summary } = list
const { byName } = useVocabularies()
const { memberText, rowText } = useRowLabel()

const members = computed<CatalogSchemaMember[]>(
  () => field.value?.element?.members ?? []
)
const cap = computed(() => field.value?.max_elements ?? 0)
const fixed = computed(() => config.value?.fixedRows === true)
const memberLabel = (key: string) => config.value?.members?.[key]?.label ?? key

const columnWidth = (member: CatalogSchemaMember) => {
  if (member.type === 'bool') {
    return '5.5rem'
  }
  if (member.type === 'ref') {
    return 'minmax(0,1.6fr)'
  }
  if (member.type === 'text') {
    return 'minmax(0,1.3fr)'
  }
  return 'minmax(0,1fr)'
}
const gridStyle = computed(() => ({
  '--row-cols': members.value.map(columnWidth).join(' ')
}))

const reorder = useListReorder(
  () => list.draftList.value,
  (next) => list.setDraft(next)
)

const preferredLang = computed(() => {
  const entity = list.context.config.entityType
  const own = list.context.editor.draft.value[`${entity}.lang`]
  const original = list.context.editor.draft.value[`${entity}.olang`]
  return [own, original, 'ja'].find(
    (lang): lang is string => typeof lang === 'string' && lang !== ''
  )
})

const emptyRow = (): Record<string, unknown> => {
  const row: Record<string, unknown> = {}
  for (const member of members.value) {
    if (member.nullable) {
      continue
    }
    const slot = memberVocabSlot(member)
    if (slot) {
      const wires = byName.value[slot.vocabulary]
        ? vocabWireValues(slot, byName.value[slot.vocabulary]!)
        : []
      const preferred =
        member.key === 'lang'
          ? wires.find((entry) => entry.token === preferredLang.value)
          : undefined
      row[member.key] = (preferred ?? wires[0])?.wire ?? null
    } else if (member.type === 'bool') {
      row[member.key] = false
    } else if (member.type === 'text') {
      row[member.key] = ''
    } else {
      row[member.key] = null
    }
  }
  return row
}

const showHidden = ref(false)
const query = ref('')
const FILTER_THRESHOLD = 12
const rowSearchText = (row: MergedRow) =>
  row.lane === 'ghost'
    ? list.ghostLabel(row)
    : rowText(props.fieldKey, row.value)
const visibleRows = computed(() => {
  const q = query.value.trim().toLowerCase()
  return rows.value.filter(
    (row) =>
      (showHidden.value || row.status !== 'hidden') &&
      (!q ||
        row.lane === 'human' ||
        rowSearchText(row).toLowerCase().includes(q))
  )
})
const hiddenCount = computed(
  () => rows.value.filter((row) => row.status === 'hidden').length
)

const valueOf = (row: MergedRow) => (row.value ?? {}) as Record<string, unknown>

const memberInvalid = (index: number, key: string) =>
  issues.value.some((issue) => issue.index === index && issue.member === key)
const rowIssues = (index: number) =>
  issues.value
    .filter((issue) => issue.index === index)
    .map((issue) =>
      issue.member
        ? `${memberLabel(issue.member)}：${issue.message}`
        : issue.message
    )

const cellText = (row: MergedRow, member: CatalogSchemaMember) =>
  memberText(props.fieldKey, member, valueOf(row)[member.key])

const canAdd = computed(
  () =>
    !disabled.value &&
    !fixed.value &&
    (!cap.value || list.draftList.value.length < cap.value)
)
</script>

<template>
  <div class="@container">
    <div v-if="rows.length > FILTER_THRESHOLD" class="mb-2 px-2">
      <KunInput
        v-model="query"
        :placeholder="`在 ${rows.length} 条中筛选`"
        :aria-label="`筛选${config?.label ?? ''}`"
        size="sm"
        is-clearable
        class="w-full @xl:w-64"
      />
    </div>
    <div
      v-if="visibleRows.length"
      class="hidden items-center gap-2 px-2 pb-1 @3xl:flex"
    >
      <span v-if="!fixed && !disabled" class="w-5 shrink-0" />
      <div
        class="text-default-400 grid min-w-0 flex-1 gap-2 text-xs @3xl:grid-cols-(--row-cols)"
        :style="gridStyle"
      >
        <span v-for="member in members" :key="member.key" class="truncate">
          {{ memberLabel(member.key) }}
          <span v-if="!member.nullable" class="text-danger-500">*</span>
        </span>
      </div>
      <span class="w-48 shrink-0" />
    </div>

    <ul v-if="visibleRows.length" class="space-y-1">
      <li
        v-for="row in visibleRows"
        :key="row.key"
        v-bind="
          row.lane === 'human' &&
          row.status !== 'removed' &&
          !disabled &&
          !fixed
            ? reorder.dropAttrs(row.index)
            : {}
        "
        :class="
          cn(
            row.lane === 'human' &&
              reorder.overIndex.value === row.index &&
              row.status !== 'removed' &&
              'ring-primary-300 rounded-lg ring-2'
          )
        "
      >
        <EditListRow
          :row="row"
          :source="sourceLabelOf(row)"
          :machine="row.upstream?.machine"
          :can-hide="list.canHide(row)"
          :can-remove="!disabled && !fixed"
          lock-reason="这条资料源记录不能单独隐藏"
          trail-class-name="w-auto justify-end @3xl:w-48"
          @remove="list.removeAt(row.index)"
          @hide="list.hide(row)"
          @restore="list.restore(row)"
          @undo="list.undo(row)"
        >
          <template v-if="!fixed && !disabled" #lead>
            <EditDragHandle
              v-if="row.lane === 'human' && row.status !== 'removed'"
              v-bind="reorder.handleAttrs(row.index)"
              @keydown="reorder.onKeydown($event, row.index)"
            />
            <span v-else class="w-5 shrink-0" />
          </template>

          <span v-if="row.lane === 'ghost'" class="text-sm break-all">
            {{ list.ghostLabel(row) }}
          </span>
          <div
            v-else-if="row.lane === 'human' && row.status !== 'removed'"
            class="grid min-w-0 gap-x-2 gap-y-1.5 @md:grid-cols-2 @3xl:grid-cols-(--row-cols) @3xl:items-center"
            :style="gridStyle"
          >
            <div
              v-for="member in members"
              :key="member.key"
              class="min-w-0 space-y-0.5"
            >
              <span class="text-default-400 block text-xs @3xl:hidden">
                {{ memberLabel(member.key) }}
              </span>
              <EditMemberInput
                :member="member"
                :config="config?.members?.[member.key]"
                :model-value="valueOf(row)[member.key]"
                :disabled="disabled || config?.members?.[member.key]?.readonly"
                :invalid="memberInvalid(row.index, member.key)"
                inline
                @update:model-value="
                  list.patchAt(row.index, { [member.key]: $event })
                "
              />
            </div>
          </div>
          <div v-else class="min-w-0">
            <p
              class="truncate text-sm leading-8 @3xl:hidden"
              :title="rowSearchText(row)"
            >
              {{ rowSearchText(row) || '—' }}
            </p>
            <div
              class="hidden min-w-0 gap-x-2 @3xl:grid @3xl:grid-cols-(--row-cols) @3xl:items-center"
              :style="gridStyle"
            >
              <span
                v-for="member in members"
                :key="member.key"
                class="text-foreground block truncate text-sm leading-8"
                :title="cellText(row, member)"
              >
                {{ cellText(row, member) || '—' }}
              </span>
            </div>
          </div>
          <p
            v-for="(message, i) in row.lane === 'human' &&
            row.status !== 'removed'
              ? rowIssues(row.index)
              : []"
            :key="i"
            class="text-danger-600 mt-1 text-xs"
          >
            {{ message }}
          </p>
        </EditListRow>
      </li>
    </ul>

    <WorkspaceEmpty
      v-else
      icon="lucide:list"
      :title="config?.emptyText ?? '还没有任何条目'"
      class-name="py-6"
    />

    <EditListHiddenToggle v-model="showHidden" :count="hiddenCount" />

    <div v-if="canAdd || cap" class="mt-2 flex items-center gap-3 px-2">
      <KunButton
        v-if="canAdd"
        variant="flat"
        size="sm"
        @click="list.append(emptyRow())"
      >
        <KunIcon name="lucide:plus" class="size-4" />
        添加{{ config?.label ?? '一行' }}
      </KunButton>
      <span class="text-default-400 text-xs tabular-nums">
        显示 {{ summary.visible }} 条
        <template v-if="summary.upstream">
          · 其中 {{ summary.upstream }} 条来自资料源
        </template>
        <template v-if="cap"> · 人工上限 {{ cap }}</template>
      </span>
    </div>
  </div>
</template>
