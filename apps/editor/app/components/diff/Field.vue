<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogSchemaField } from '#shared/types/catalog'
import type { DiffContext } from '~/utils/review/context'
import {
  imageUrl,
  refCollectionOf,
  scalarText,
  vocabularyLabel
} from '~/utils/review/describe'
import {
  alignList,
  asList,
  diffProse,
  diffText,
  naturalKeyMembers,
  summarizeList,
  type ListEntry
} from '#shared/utils/diff'

const props = withDefaults(
  defineProps<{
    field: CatalogSchemaField
    before: unknown
    after: unknown
    context: DiffContext
    beforeLabel?: string
    afterLabel?: string
  }>(),
  { beforeLabel: '当前', afterLabel: '提议' }
)

const STATUS: Record<ListEntry['status'], { label: string; tone: string }> = {
  added: { label: '新增', tone: 'border-success-300 bg-success-50' },
  removed: { label: '删除', tone: 'border-danger-300 bg-danger-50' },
  changed: { label: '修改', tone: 'border-warning-300 bg-warning-50' },
  moved: { label: '移动', tone: 'border-info-300 bg-info-50' },
  kept: { label: '不变', tone: 'border-default-200' }
}

const isList = computed(() => props.field.field_type === 'list')
const members = computed(() => props.field.element?.members ?? [])
const isImageList = computed(
  () =>
    isList.value &&
    (props.field.diff_hint === 'image' ||
      members.value.some((member) => member.type === 'imagehash'))
)

const booleanField = computed(
  () => typeof props.before === 'boolean' || typeof props.after === 'boolean'
)

const scalarDisplay = (value: unknown) => {
  if (booleanField.value && (value === null || value === undefined)) {
    return '未知'
  }
  if (props.field.field_type === 'ref') {
    const collection = refCollectionOf(props.field.key)
    return value === null || value === undefined
      ? '—'
      : collection
        ? props.context.refName(collection, value)
        : `#${String(value)}`
  }
  if (props.field.vocabulary) {
    return vocabularyLabel(
      value,
      props.context.vocabulary(props.field.vocabulary),
      props.field.encoding,
      props.field.base
    )
  }
  return scalarText(value)
}

const textual = computed(
  () =>
    props.field.field_type === 'text' &&
    (typeof props.before === 'string' || typeof props.after === 'string')
)
const textSegments = computed(() => {
  const before = typeof props.before === 'string' ? props.before : ''
  const after = typeof props.after === 'string' ? props.after : ''
  return props.field.diff_hint === 'lines'
    ? diffProse(before, after)
    : diffText(before, after)
})

const entries = computed(() =>
  isList.value
    ? alignList(
        asList(props.before),
        asList(props.after),
        naturalKeyMembers(props.field.key, members.value)
      )
    : []
)
const summary = computed(() => summarizeList(entries.value))
const showKept = ref(false)
const visibleEntries = computed(() =>
  showKept.value
    ? entries.value
    : entries.value.filter((entry) => entry.status !== 'kept')
)
const keptCount = computed(
  () => entries.value.filter((entry) => entry.status === 'kept').length
)

const longTextMember = computed(
  () =>
    members.value.find(
      (member) =>
        member.type === 'text' &&
        ['intro', 'description', 'note'].includes(member.key)
    )?.key ?? null
)

const memberSegments = (entry: ListEntry) => {
  const key = longTextMember.value
  if (!key || entry.status !== 'changed') {
    return null
  }
  const before = String((entry.before as Record<string, unknown>)?.[key] ?? '')
  const after = String((entry.after as Record<string, unknown>)?.[key] ?? '')
  return before === after ? null : diffProse(before, after)
}

const imageOf = (item: unknown) => {
  const member = members.value.find((m) => m.type === 'imagehash')
  if (!member || !item || typeof item !== 'object') {
    return ''
  }
  return imageUrl((item as Record<string, unknown>)[member.key])
}
</script>

<template>
  <div class="text-sm">
    <DiffWalkthrough
      v-if="props.field.diff_hint === 'tree'"
      :before="props.before"
      :after="props.after"
    />
    <div v-else-if="!isList && textual" class="grid gap-3 md:grid-cols-2">
      <div class="border-default-200 rounded-md border p-3">
        <p class="text-default-400 mb-1 text-xs">{{ props.beforeLabel }}</p>
        <DiffText :segments="textSegments" side="before" />
      </div>
      <div class="border-default-200 rounded-md border p-3">
        <p class="text-default-400 mb-1 text-xs">{{ props.afterLabel }}</p>
        <DiffText :segments="textSegments" side="after" />
      </div>
    </div>

    <div v-else-if="!isList" class="grid gap-3 md:grid-cols-2">
      <div class="border-default-200 rounded-md border p-3">
        <p class="text-default-400 mb-1 text-xs">{{ props.beforeLabel }}</p>
        <div
          v-if="
            props.field.field_type === 'imagehash' && imageUrl(props.before)
          "
          class="bg-default-100 w-24 overflow-hidden rounded"
        >
          <KunImage :src="imageUrl(props.before)" alt="" object-fit="cover" />
        </div>
        <span v-else class="text-danger-700 break-words">{{
          scalarDisplay(props.before)
        }}</span>
      </div>
      <div class="border-default-200 rounded-md border p-3">
        <p class="text-default-400 mb-1 text-xs">{{ props.afterLabel }}</p>
        <div
          v-if="props.field.field_type === 'imagehash' && imageUrl(props.after)"
          class="bg-default-100 w-24 overflow-hidden rounded"
        >
          <KunImage :src="imageUrl(props.after)" alt="" object-fit="cover" />
        </div>
        <span v-else class="text-success-700 break-words">{{
          scalarDisplay(props.after)
        }}</span>
      </div>
    </div>

    <div v-else class="space-y-2">
      <p class="text-default-500 flex flex-wrap gap-x-3 text-xs">
        <span v-if="summary.added" class="text-success-600"
          >+{{ summary.added }} 新增</span
        >
        <span v-if="summary.removed" class="text-danger-600"
          >−{{ summary.removed }} 删除</span
        >
        <span v-if="summary.changed" class="text-warning-600"
          >{{ summary.changed }} 修改</span
        >
        <span v-if="summary.moved" class="text-info-600"
          >{{ summary.moved }} 调整顺序</span
        >
        <span
          v-if="
            !summary.added &&
            !summary.removed &&
            !summary.changed &&
            !summary.moved
          "
          >无实际变化</span
        >
      </p>

      <div
        v-if="isImageList"
        class="grid grid-cols-3 gap-2 sm:grid-cols-4 lg:grid-cols-6"
      >
        <div
          v-for="(entry, index) in visibleEntries"
          :key="index"
          :class="
            cn('overflow-hidden rounded-md border-2', STATUS[entry.status].tone)
          "
        >
          <KunImage
            :src="imageOf(entry.after ?? entry.before)"
            alt=""
            aspect-ratio="3 / 4"
            object-fit="cover"
          />
          <p class="px-1.5 py-1 text-xs">
            <span class="font-medium">{{ STATUS[entry.status].label }}</span>
            <DiffElement
              v-if="entry.status === 'changed' || entry.status === 'added'"
              :field="props.field"
              :item="entry.after"
              :context="props.context"
              class="mt-0.5 block"
            />
          </p>
        </div>
      </div>

      <ul v-else class="space-y-1.5">
        <li
          v-for="(entry, index) in visibleEntries"
          :key="index"
          :class="cn('rounded-md border px-3 py-2', STATUS[entry.status].tone)"
        >
          <div class="flex items-start gap-2">
            <span class="text-default-600 mt-0.5 shrink-0 text-xs font-medium">
              {{ STATUS[entry.status].label }}
            </span>
            <div class="min-w-0 flex-1 space-y-1">
              <template v-if="entry.status === 'changed'">
                <div v-if="memberSegments(entry)" class="space-y-1">
                  <DiffElement
                    :field="props.field"
                    :item="entry.after"
                    :context="props.context"
                    :omit="longTextMember ? [longTextMember] : []"
                  />
                  <DiffText :segments="memberSegments(entry)!" />
                </div>
                <template v-else>
                  <div class="text-danger-700 line-through decoration-1">
                    <DiffElement
                      :field="props.field"
                      :item="entry.before"
                      :context="props.context"
                    />
                  </div>
                  <div>
                    <DiffElement
                      :field="props.field"
                      :item="entry.after"
                      :context="props.context"
                    />
                  </div>
                </template>
              </template>
              <DiffElement
                v-else
                :field="props.field"
                :item="entry.after ?? entry.before"
                :context="props.context"
              />
            </div>
          </div>
        </li>
      </ul>

      <KunButton
        v-if="keptCount"
        variant="light"
        size="sm"
        @click="showKept = !showKept"
      >
        {{ showKept ? '隐藏未变化的项' : `显示未变化的 ${keptCount} 项` }}
      </KunButton>
    </div>
  </div>
</template>
