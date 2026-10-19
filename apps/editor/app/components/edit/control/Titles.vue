<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaMember } from '#shared/types/edit'

const props = defineProps<{ fieldKey: string }>()

const {
  value,
  baseValue,
  disabled,
  config,
  field,
  issues,
  companion,
  suppressed
} = useEditorField(() => props.fieldKey)

type Row = Record<string, unknown>

const members = computed<Record<string, CatalogSchemaMember>>(() =>
  Object.fromEntries(
    (field.value?.element?.members ?? []).map((m) => [m.key, m])
  )
)
const rows = computed<Row[]>(() =>
  Array.isArray(value.value) ? (value.value as Row[]) : []
)
const baseRows = computed<Row[]>(() =>
  Array.isArray(baseValue.value) ? (baseValue.value as Row[]) : []
)
const identityOf = (row: Row) => rowIdentity(props.fieldKey, row)
const baseIdentities = computed(() => new Set(baseRows.value.map(identityOf)))
const draftIdentities = computed(() => new Set(rows.value.map(identityOf)))

const removedThisTime = computed(() =>
  baseRows.value.filter((row) => !draftIdentities.value.has(identityOf(row)))
)
const removedIdentities = computed(
  () => new Set(removedThisTime.value.map(identityOf))
)
const olderSuppressions = computed(() =>
  suppressed.value
    .filter((key) => !removedIdentities.value.has(key))
    .map((key) => ({ key, label: suppressedKeyParts(key).label }))
)

const update = (next: Row[]) => {
  value.value = next
}
const reorder = useListReorder(() => rows.value, update)

const setMember = (index: number, key: string, next: unknown) => {
  update(
    rows.value.map((row, i) => (i === index ? { ...row, [key]: next } : row))
  )
}

const addRow = () => update([...rows.value, { lang: '', title: '', kind: 1 }])
const restore = (row: Row) => update([...rows.value, cloneValue(row)])
const unsuppress = (key: string) => {
  suppressed.value = suppressed.value.filter((entry) => entry !== key)
}

const memberInvalid = (index: number, key: string) =>
  issues.value.some((issue) => issue.index === index && issue.member === key)
const rowIssues = (index: number) =>
  issues.value.filter((issue) => issue.index === index && !issue.member)
const cap = computed(() => field.value?.max_elements ?? 0)
</script>

<template>
  <div>
    <div class="space-y-2">
      <div
        v-for="(row, index) in rows"
        :key="index"
        v-bind="disabled ? {} : reorder.dropAttrs(index)"
        :class="
          cn(
            'border-default-200 rounded-lg border p-2',
            !baseIdentities.has(identityOf(row)) &&
              'border-success-300 bg-success-50',
            reorder.overIndex.value === index && 'ring-primary-300 ring-2'
          )
        "
      >
        <div class="flex items-start gap-2">
          <EditDragHandle
            v-if="!disabled"
            v-bind="reorder.handleAttrs(index)"
            class="mt-1.5"
            @keydown="reorder.onKeydown($event, index)"
          />
          <div
            class="grid min-w-0 flex-1 gap-2 sm:grid-cols-[minmax(0,1fr)_9rem] lg:grid-cols-[minmax(0,2fr)_9rem_8rem_minmax(0,1fr)]"
          >
            <EditMemberInput
              v-if="members.title"
              :member="members.title"
              :config="config?.members?.title"
              :model-value="row.title"
              :disabled="disabled"
              :invalid="memberInvalid(index, 'title')"
              @update:model-value="setMember(index, 'title', $event)"
            />
            <EditMemberInput
              v-if="members.lang"
              :member="members.lang"
              :config="config?.members?.lang"
              :model-value="row.lang"
              :disabled="disabled"
              @update:model-value="setMember(index, 'lang', $event ?? '')"
            />
            <EditMemberInput
              v-if="members.kind"
              :member="members.kind"
              :config="config?.members?.kind"
              :model-value="row.kind"
              :disabled="disabled"
              @update:model-value="setMember(index, 'kind', $event)"
            />
            <EditMemberInput
              v-if="members.latin"
              :member="members.latin"
              :config="config?.members?.latin"
              :model-value="row.latin"
              :disabled="disabled"
              @update:model-value="setMember(index, 'latin', $event)"
            />
          </div>
          <EditRowActions
            :index="index"
            :count="rows.length"
            :disabled="disabled"
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

    <div v-if="!disabled" class="mt-3 flex items-center gap-2">
      <KunButton
        variant="flat"
        size="sm"
        :disabled="cap > 0 && rows.length >= cap"
        @click="addRow"
      >
        <KunIcon name="lucide:plus" class="size-4" />
        添加标题
      </KunButton>
    </div>

    <div
      v-if="removedThisTime.length"
      class="border-warning-200 bg-warning-50 mt-3 rounded-lg border px-3 py-2"
    >
      <p class="text-warning-700 mb-1 flex items-center gap-1.5 text-sm">
        <KunIcon name="lucide:eye-off" class="size-4" />
        本次删除的标题{{
          companion ? '（会同时屏蔽，资料源同步不会再加回）' : ''
        }}
      </p>
      <ul class="divide-warning-100 divide-y">
        <li
          v-for="row in removedThisTime"
          :key="identityOf(row)"
          class="flex items-center gap-2 py-1.5 text-sm"
        >
          <span class="min-w-0 flex-1 break-all">
            {{ row.title }}
            <span class="text-default-400 text-xs">{{
              row.lang || '无语言'
            }}</span>
          </span>
          <KunButton
            size="xs"
            variant="light"
            color="primary"
            @click="restore(row)"
          >
            恢复
          </KunButton>
        </li>
      </ul>
    </div>

    <div
      v-if="companion && olderSuppressions.length"
      class="border-default-200 bg-default-50 mt-3 rounded-lg border px-3 py-2"
    >
      <p class="text-default-600 mb-1 flex items-center gap-1.5 text-sm">
        <KunIcon name="lucide:shield-off" class="size-4" />
        已屏蔽的标题
        <span class="text-default-400 tabular-nums">{{
          olderSuppressions.length
        }}</span>
      </p>
      <ul class="divide-default-200 divide-y">
        <li
          v-for="entry in olderSuppressions"
          :key="entry.key"
          class="flex items-center gap-2 py-1.5 text-sm"
        >
          <span class="text-default-500 min-w-0 flex-1 break-all">{{
            entry.label
          }}</span>
          <KunButton
            v-if="!disabled"
            size="xs"
            variant="light"
            color="primary"
            @click="unsuppress(entry.key)"
          >
            取消屏蔽
          </KunButton>
        </li>
      </ul>
    </div>
  </div>
</template>
