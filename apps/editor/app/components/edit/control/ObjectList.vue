<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaMember } from '#shared/types/edit'

const props = defineProps<{ fieldKey: string }>()

const { value, baseValue, disabled, config, field, issues } = useEditorField(
  () => props.fieldKey
)
const { byName } = useVocabularies()

type Row = Record<string, unknown>

const members = computed<CatalogSchemaMember[]>(
  () => field.value?.element?.members ?? []
)
const rows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseIdentities = computed(
  () =>
    new Set(
      (Array.isArray(baseValue.value) ? baseValue.value : []).map((row) =>
        rowIdentity(props.fieldKey, row)
      )
    )
)
const cap = computed(() => field.value?.max_elements ?? 0)

const update = (next: Row[]) => {
  value.value = next
}
const reorder = useListReorder(() => rows.value, update)

const setMember = (index: number, key: string, next: unknown) => {
  update(
    rows.value.map((row, i) => (i === index ? { ...row, [key]: next } : row))
  )
}

const emptyRow = (): Row => {
  const row: Row = {}
  for (const member of members.value) {
    if (member.nullable) {
      continue
    }
    const slot = memberVocabSlot(member)
    if (slot) {
      const first = byName.value[slot.vocabulary]
        ? vocabWireValues(slot, byName.value[slot.vocabulary]!)[0]?.wire
        : undefined
      row[member.key] = first ?? null
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

const rowIssues = (index: number) =>
  issues.value
    .filter((issue) => issue.index === index)
    .map((issue) => ({
      ...issue,
      message: issue.member
        ? `${config.value?.members?.[issue.member]?.label ?? issue.member}：${issue.message}`
        : issue.message
    }))
const memberInvalid = (index: number, key: string) =>
  issues.value.some((issue) => issue.index === index && issue.member === key)

const isNew = (row: Row) =>
  !baseIdentities.value.has(rowIdentity(props.fieldKey, row))

const excluded = computed(() => [...baseIdentities.value])
const fixed = computed(() => config.value?.fixedRows === true)
</script>

<template>
  <div>
    <div v-if="rows.length" class="space-y-2">
      <div
        v-for="(row, index) in rows"
        :key="index"
        v-bind="disabled || fixed ? {} : reorder.dropAttrs(index)"
        :class="
          cn(
            'border-default-200 rounded-lg border p-2',
            isNew(row) && 'border-success-300 bg-success-50',
            reorder.overIndex.value === index && 'ring-primary-300 ring-2'
          )
        "
      >
        <div class="flex items-start gap-2">
          <EditDragHandle
            v-if="!disabled && !fixed"
            v-bind="reorder.handleAttrs(index)"
            class="mt-6"
            @keydown="reorder.onKeydown($event, index)"
          />
          <div class="grid min-w-0 flex-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
            <div
              v-for="member in members"
              :key="member.key"
              class="min-w-0 space-y-1"
            >
              <span class="text-default-500 text-xs">
                {{ config?.members?.[member.key]?.label ?? member.key }}
                <span v-if="!member.nullable" class="text-danger-500">*</span>
              </span>
              <EditMemberInput
                :member="member"
                :config="config?.members?.[member.key]"
                :model-value="row[member.key]"
                :disabled="disabled || config?.members?.[member.key]?.readonly"
                :invalid="memberInvalid(index, member.key)"
                @update:model-value="setMember(index, member.key, $event)"
              />
            </div>
          </div>
          <EditRowActions
            v-if="!fixed"
            :index="index"
            :count="rows.length"
            :disabled="disabled"
            class="mt-5"
            @move="reorder.move"
            @remove="update(rows.filter((_, i) => i !== index))"
          />
        </div>
        <p
          v-for="(issue, i) in rowIssues(index)"
          :key="i"
          class="text-danger-600 mt-1 pl-6 text-xs"
        >
          {{ issue.message }}
        </p>
      </div>
    </div>
    <p v-else class="text-default-400 text-sm">
      {{ config?.emptyText ?? '还没有人工添加的条目。' }}
    </p>

    <div v-if="!disabled && !fixed" class="mt-3 flex items-center gap-2">
      <KunButton
        variant="flat"
        size="sm"
        :disabled="cap > 0 && rows.length >= cap"
        @click="update([...rows, emptyRow()])"
      >
        <KunIcon name="lucide:plus" class="size-4" />
        添加一行
      </KunButton>
      <span v-if="cap" class="text-default-400 text-xs tabular-nums">
        {{ rows.length }} / {{ cap }}
      </span>
    </div>

    <EditUpstream :field-key="props.fieldKey" :exclude="excluded" />
    <EditSuppressedList :field-key="props.fieldKey" />
  </div>
</template>
