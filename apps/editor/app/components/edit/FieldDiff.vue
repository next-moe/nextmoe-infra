<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { vocabLabel } from '~/constants/vocabLabels'

const props = withDefaults(
  defineProps<{ fieldKey: string; conflict?: boolean; bare?: boolean }>(),
  { conflict: false, bare: false }
)

const { context, field, config } = useEditorField(() => props.fieldKey)
const { rowText, scalarText, refsInValue } = useRowLabel()
const editor = context.editor
const refs = context.refs

const before = computed(() =>
  field.value
    ? normalizeFieldValue(field.value, editor.base.value[props.fieldKey])
    : undefined
)
const after = computed(() =>
  field.value
    ? normalizeFieldValue(field.value, editor.draft.value[props.fieldKey])
    : undefined
)

const suppressed = computed(() => isSuppressedKey(props.fieldKey))
const label = computed(() => {
  if (suppressed.value) {
    const parent = props.fieldKey.slice(0, -'.suppressed'.length)
    return `${context.config.fields[parent]?.label ?? fieldLabel(parent)} · 资料源记录`
  }
  return config.value?.label ?? fieldLabel(props.fieldKey)
})

const isList = computed(() => field.value?.field_type === 'list')
const isIntros = computed(() => props.fieldKey.endsWith('.intros'))
const isMediaOrder = computed(() =>
  /\.(cover|screenshot)_order$/.test(props.fieldKey)
)
const isImageList = computed(() =>
  (field.value?.element?.members ?? []).some(
    (member) => member.type === 'imagehash'
  )
)

const listDiff = computed(() =>
  isList.value
    ? diffFieldList(
        props.fieldKey,
        (before.value as unknown[]) ?? [],
        (after.value as unknown[]) ?? []
      )
    : null
)

const imageOf = (row: unknown) => {
  const hash = String((row as Record<string, unknown> | null)?.image_hash ?? '')
  const known = context.images.value[hash]
  return known ? known.url : hash ? imageUrl(hash) : ''
}

type IntroRow = { lang: string; intro: string }
const introDiffs = computed(() => {
  if (!isIntros.value) {
    return []
  }
  const prev = new Map(
    ((before.value as IntroRow[]) ?? []).map((r) => [r.lang, r.intro])
  )
  const next = new Map(
    ((after.value as IntroRow[]) ?? []).map((r) => [r.lang, r.intro])
  )
  return [...new Set([...prev.keys(), ...next.keys()])]
    .filter((lang) => prev.get(lang) !== next.get(lang))
    .map((lang) => ({
      lang,
      before: prev.get(lang) ?? '',
      after: next.get(lang) ?? ''
    }))
})

const ADDED_TONE = {
  box: 'border-success-300 bg-success-50',
  row: 'bg-success-50 text-success-800',
  label: 'text-success-600'
}
const REMOVED_TONE = {
  box: 'border-danger-300 bg-danger-50',
  row: 'bg-danger-50 text-danger-800',
  label: 'text-danger-600'
}
const HIDDEN_TONE = {
  box: 'border-default-300 bg-default-100',
  row: 'bg-default-100 text-default-700',
  label: 'text-default-500'
}
const RESTORED_TONE = {
  box: 'border-primary-300 bg-primary-50',
  row: 'bg-primary-50 text-primary-800',
  label: 'text-primary-600'
}
const addedTone = computed(() => (suppressed.value ? HIDDEN_TONE : ADDED_TONE))
const removedTone = computed(() =>
  suppressed.value ? RESTORED_TONE : REMOVED_TONE
)
const addedLabel = computed(() => (suppressed.value ? '隐藏' : '新增'))
const removedLabel = computed(() => (suppressed.value ? '恢复显示' : '删除'))

watch(
  [before, after],
  () => {
    for (const ref of [
      ...refsInValue(props.fieldKey, before.value),
      ...refsInValue(props.fieldKey, after.value)
    ]) {
      refs.resolve(ref.family, [ref.id])
    }
  },
  { immediate: true }
)
</script>

<template>
  <section
    :class="cn(!props.bare && 'border-default-200 rounded-lg border p-3')"
  >
    <header class="mb-2 flex flex-wrap items-center gap-2">
      <h4 class="text-sm font-semibold">{{ label }}</h4>
      <KunChip v-if="props.conflict" size="xs" variant="flat" color="danger">
        期间已被他人修改
      </KunChip>
      <slot name="actions" />
    </header>

    <div v-if="isIntros" class="space-y-3">
      <div v-for="entry in introDiffs" :key="entry.lang" class="space-y-1">
        <p class="text-default-500 flex items-center gap-1.5 text-xs">
          {{ vocabLabel('intro_lang', entry.lang) }}
          <KunChip
            v-if="!entry.before"
            size="xs"
            variant="flat"
            color="success"
          >
            新增
          </KunChip>
          <KunChip
            v-else-if="!entry.after"
            size="xs"
            variant="flat"
            color="danger"
          >
            删除
          </KunChip>
        </p>
        <EditTextDiff :before="entry.before" :after="entry.after" />
      </div>
    </div>

    <EditMediaOrderDiff
      v-else-if="isMediaOrder"
      :field-key="props.fieldKey"
      :before="before"
      :after="after"
    />

    <div v-else-if="listDiff && isImageList" class="space-y-2">
      <div
        v-if="listDiff.added.length || listDiff.removed.length"
        class="flex flex-wrap gap-2"
      >
        <figure
          v-for="(row, i) in listDiff.added"
          :key="`a${i}`"
          :class="cn('w-24 overflow-hidden rounded-lg border', addedTone.box)"
        >
          <KunImage
            v-if="imageOf(row)"
            :src="imageOf(row)"
            alt=""
            aspect-ratio="1 / 1"
            object-fit="cover"
          />
          <figcaption :class="cn('px-1.5 py-0.5 text-[11px]', addedTone.label)">
            {{ addedLabel }}
          </figcaption>
        </figure>
        <figure
          v-for="(row, i) in listDiff.removed"
          :key="`r${i}`"
          :class="cn('w-24 overflow-hidden rounded-lg border', removedTone.box)"
        >
          <KunImage
            v-if="imageOf(row)"
            :src="imageOf(row)"
            alt=""
            aspect-ratio="1 / 1"
            object-fit="cover"
            class-name="opacity-60"
          />
          <figcaption
            :class="cn('px-1.5 py-0.5 text-[11px]', removedTone.label)"
          >
            {{ removedLabel }}
          </figcaption>
        </figure>
      </div>
      <div
        v-for="(pair, i) in listDiff.changed"
        :key="`c${i}`"
        class="border-warning-200 bg-warning-50 flex items-center gap-3 rounded-lg border p-2 text-sm"
      >
        <KunImage
          v-if="imageOf(pair.after)"
          :src="imageOf(pair.after)"
          alt=""
          aspect-ratio="1 / 1"
          object-fit="cover"
          class-name="size-12 shrink-0 rounded"
        />
        <EditMediaRowChange
          :before="pair.before as Record<string, unknown>"
          :after="pair.after as Record<string, unknown>"
        />
      </div>
      <p
        v-if="listDiff.reordered"
        class="text-default-500 flex items-center gap-1.5 text-xs"
      >
        <KunIcon name="lucide:arrow-up-down" class="size-3.5" />
        调整了顺序
      </p>
    </div>

    <ul v-else-if="listDiff" class="space-y-1 text-sm">
      <li
        v-for="(row, i) in listDiff.added"
        :key="`a${i}`"
        :class="
          cn('flex items-start gap-2 rounded-md px-2 py-1', addedTone.row)
        "
      >
        <span :class="cn('shrink-0 text-xs leading-5', addedTone.label)">
          {{ addedLabel }}
        </span>
        <span class="break-all">{{ rowText(props.fieldKey, row) }}</span>
      </li>
      <li
        v-for="(row, i) in listDiff.removed"
        :key="`r${i}`"
        :class="
          cn('flex items-start gap-2 rounded-md px-2 py-1', removedTone.row)
        "
      >
        <span :class="cn('shrink-0 text-xs leading-5', removedTone.label)">
          {{ removedLabel }}
        </span>
        <span :class="cn('break-all', !suppressed && 'line-through')">{{
          rowText(props.fieldKey, row)
        }}</span>
      </li>
      <li
        v-for="(pair, i) in listDiff.changed"
        :key="`c${i}`"
        class="bg-warning-50 flex items-start gap-2 rounded-md px-2 py-1"
      >
        <span class="text-warning-700 shrink-0 text-xs leading-5">修改</span>
        <span class="break-all">
          <span class="text-default-400 line-through">{{
            rowText(props.fieldKey, pair.before)
          }}</span>
          <KunIcon
            name="lucide:arrow-right"
            class="text-default-400 mx-1 inline size-3"
          />
          <span>{{ rowText(props.fieldKey, pair.after) }}</span>
        </span>
      </li>
      <li
        v-if="listDiff.reordered"
        class="text-default-500 flex items-center gap-1.5 px-2 text-xs"
      >
        <KunIcon name="lucide:arrow-up-down" class="size-3.5" />
        调整了顺序
      </li>
    </ul>

    <EditTextDiff
      v-else-if="
        field?.field_type === 'text' && String(after ?? '').length > 40
      "
      :before="String(before ?? '')"
      :after="String(after ?? '')"
    />

    <p v-else-if="field" class="flex flex-wrap items-center gap-2 text-sm">
      <span
        class="bg-danger-50 text-danger-800 rounded-md px-2 py-0.5 line-through"
      >
        {{ scalarText(field, before) }}
      </span>
      <KunIcon name="lucide:arrow-right" class="text-default-400 size-3.5" />
      <span class="bg-success-50 text-success-800 rounded-md px-2 py-0.5">
        {{ scalarText(field, after) }}
      </span>
    </p>
  </section>
</template>
