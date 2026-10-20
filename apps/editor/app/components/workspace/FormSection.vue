<script setup lang="ts">
import { cn } from '@kungal/ui-core'
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
  defineProps<{ fields: string[]; className?: string }>(),
  { className: '' }
)

const { editor, config } = useEditorContext()

const controlOf = (key: string) => {
  const field = editor.fieldByKey.value.get(key)
  return field ? resolveEditControl(field, config.fields[key]) : 'readonly'
}

const visible = computed(() =>
  props.fields.filter(
    (key) => editor.fieldByKey.value.has(key) && !isSuppressedKey(key)
  )
)
const rowKeys = computed(() =>
  visible.value.filter((key) => ROW_CONTROLS.has(controlOf(key)))
)
const blockKeys = computed(() =>
  visible.value.filter((key) => !ROW_CONTROLS.has(controlOf(key)))
)
</script>

<template>
  <div :class="cn('space-y-4', props.className)">
    <div
      v-if="rowKeys.length"
      class="border-default-200 bg-content1 rounded-xl border px-5"
    >
      <div class="divide-default-100 divide-y">
        <EditField
          v-for="key in rowKeys"
          :key="key"
          :field-key="key"
          layout="row"
        />
      </div>
    </div>
    <div
      v-for="key in blockKeys"
      :key="key"
      class="border-default-200 bg-content1 rounded-xl border px-5"
    >
      <EditField :field-key="key" layout="block" />
    </div>
  </div>
</template>
