<script setup lang="ts">
import type { EditControl } from '#shared/types/edit'

const ROW_CONTROLS = new Set<EditControl>([
  'text',
  'vocab',
  'switch',
  'int',
  'ref',
  'date',
  'readonly'
])

const props = withDefaults(
  defineProps<{
    fieldKey: string
    layout?: 'row' | 'block' | 'auto'
    sectionLabel?: string
  }>(),
  { layout: 'auto', sectionLabel: '' }
)

const { field, config } = useEditorField(() => props.fieldKey)

const control = computed(() =>
  field.value ? resolveEditControl(field.value, config.value) : 'readonly'
)
const INLINE_ROW_ISSUES = new Set<EditControl>([
  'objectList',
  'titles',
  'intros'
])

const resolvedLayout = computed(() =>
  props.layout === 'auto'
    ? ROW_CONTROLS.has(control.value)
      ? 'row'
      : 'block'
    : props.layout
)
</script>

<template>
  <EditFieldShell
    v-if="field"
    :field-key="props.fieldKey"
    :layout="resolvedLayout"
    :section-label="props.sectionLabel"
    :inline-row-issues="INLINE_ROW_ISSUES.has(control)"
  >
    <EditControlText v-if="control === 'text'" :field-key="props.fieldKey" />
    <EditControlText
      v-else-if="control === 'textarea'"
      :field-key="props.fieldKey"
      multiline
    />
    <EditControlVocab
      v-else-if="control === 'vocab'"
      :field-key="props.fieldKey"
    />
    <EditControlVocabList
      v-else-if="control === 'vocabList'"
      :field-key="props.fieldKey"
    />
    <EditControlSwitch
      v-else-if="control === 'switch'"
      :field-key="props.fieldKey"
    />
    <EditControlInt v-else-if="control === 'int'" :field-key="props.fieldKey" />
    <EditControlRef v-else-if="control === 'ref'" :field-key="props.fieldKey" />
    <EditControlRefList
      v-else-if="control === 'refList'"
      :field-key="props.fieldKey"
    />
    <EditControlStringList
      v-else-if="control === 'stringList'"
      :field-key="props.fieldKey"
    />
    <EditControlObjectList
      v-else-if="control === 'objectList'"
      :field-key="props.fieldKey"
    />
    <EditControlTitles
      v-else-if="control === 'titles'"
      :field-key="props.fieldKey"
    />
    <EditControlIntros
      v-else-if="control === 'intros'"
      :field-key="props.fieldKey"
    />
    <EditControlImages
      v-else-if="control === 'images'"
      :field-key="props.fieldKey"
    />
    <EditControlRegrade
      v-else-if="control === 'regrade'"
      :field-key="props.fieldKey"
    />
    <EditControlRoster
      v-else-if="control === 'roster'"
      :field-key="props.fieldKey"
    />
    <EditControlDate
      v-else-if="control === 'date'"
      :field-key="props.fieldKey"
    />
    <EditControlImage
      v-else-if="control === 'image'"
      :field-key="props.fieldKey"
    />
    <EditControlReadonly v-else :field-key="props.fieldKey" />
  </EditFieldShell>
</template>
