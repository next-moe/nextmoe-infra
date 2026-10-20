<script setup lang="ts">
import { vocabLabel } from '~/constants/vocabLabels'

const props = defineProps<{ fieldKey: string; conflict?: boolean }>()

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

const label = computed(() => {
  if (isSuppressedKey(props.fieldKey)) {
    const parent = props.fieldKey.slice(0, -'.suppressed'.length)
    return `${context.config.fields[parent]?.label ?? fieldLabel(parent)} · 隐藏的资料源条目`
  }
  return config.value?.label ?? fieldLabel(props.fieldKey)
})

const isList = computed(() => field.value?.field_type === 'list')
const isIntros = computed(() => props.fieldKey.endsWith('.intros'))

const listDiff = computed(() =>
  isList.value
    ? diffFieldList(
        props.fieldKey,
        (before.value as unknown[]) ?? [],
        (after.value as unknown[]) ?? []
      )
    : null
)

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
  const langs = [...new Set([...prev.keys(), ...next.keys()])]
  return langs
    .filter((lang) => prev.get(lang) !== next.get(lang))
    .map((lang) => ({
      lang,
      before: prev.get(lang) ?? '',
      after: next.get(lang) ?? ''
    }))
})

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
  <section class="border-default-200 rounded-lg border p-3">
    <header class="mb-2 flex flex-wrap items-center gap-2">
      <h4 class="text-sm font-semibold">{{ label }}</h4>
      <KunChip v-if="props.conflict" size="xs" variant="flat" color="danger">
        期间已被他人修改
      </KunChip>
    </header>

    <div v-if="isIntros" class="space-y-3">
      <div v-for="entry in introDiffs" :key="entry.lang" class="space-y-1">
        <p class="text-default-500 text-xs">
          简介 · {{ vocabLabel('intro_lang', entry.lang) }}
          <span v-if="!entry.before" class="text-success-600">（新增）</span>
          <span v-else-if="!entry.after" class="text-danger-600">（删除）</span>
        </p>
        <EditTextDiff :before="entry.before" :after="entry.after" />
      </div>
    </div>

    <div v-else-if="listDiff" class="space-y-1 text-sm">
      <p
        v-for="(row, i) in listDiff.added"
        :key="`a${i}`"
        class="text-success-700 flex items-start gap-1.5"
      >
        <KunIcon name="lucide:plus" class="mt-0.5 size-3.5 shrink-0" />
        <span class="break-all">{{ rowText(props.fieldKey, row) }}</span>
      </p>
      <p
        v-for="(row, i) in listDiff.removed"
        :key="`r${i}`"
        class="text-danger-700 flex items-start gap-1.5"
      >
        <KunIcon name="lucide:minus" class="mt-0.5 size-3.5 shrink-0" />
        <span class="break-all line-through">{{
          rowText(props.fieldKey, row)
        }}</span>
      </p>
      <div
        v-for="(pair, i) in listDiff.changed"
        :key="`c${i}`"
        class="flex items-start gap-1.5"
      >
        <KunIcon
          name="lucide:pencil"
          class="text-warning-600 mt-0.5 size-3.5 shrink-0"
        />
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
      </div>
      <p
        v-if="listDiff.reordered"
        class="text-default-500 flex items-center gap-1.5 text-xs"
      >
        <KunIcon name="lucide:arrow-up-down" class="size-3.5" />
        调整了顺序
      </p>
    </div>

    <div
      v-else-if="
        field?.field_type === 'text' && String(after ?? '').length > 60
      "
    >
      <EditTextDiff
        :before="String(before ?? '')"
        :after="String(after ?? '')"
      />
    </div>

    <p v-else-if="field" class="flex flex-wrap items-center gap-1.5 text-sm">
      <span class="text-default-400 line-through">{{
        scalarText(field, before)
      }}</span>
      <KunIcon name="lucide:arrow-right" class="text-default-400 size-3.5" />
      <span class="font-medium">{{ scalarText(field, after) }}</span>
    </p>
  </section>
</template>
