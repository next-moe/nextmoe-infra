<script setup lang="ts">
import type { CatalogSchemaField } from '#shared/types/catalog'
import type { EditFieldConfig } from '#shared/types/edit'

const props = withDefaults(
  defineProps<{
    field: CatalogSchemaField
    config?: EditFieldConfig
    modelValue: unknown
    required?: boolean
  }>(),
  { config: undefined, required: false }
)

const emit = defineEmits<{ 'update:modelValue': [value: unknown] }>()

const label = computed(() => props.config?.label ?? props.field.key)
const slot = computed(() => fieldVocabSlot(props.field))
const refFamily = computed(() =>
  props.field.field_type === 'ref' ? props.config?.ref : undefined
)

const textValue = computed({
  get: () =>
    props.modelValue === null || props.modelValue === undefined
      ? ''
      : String(props.modelValue),
  set: (next: string | number) => emit('update:modelValue', String(next))
})

const numberValue = computed({
  get: () =>
    props.modelValue === null || props.modelValue === undefined
      ? null
      : Number(props.modelValue),
  set: (next: number | null) => emit('update:modelValue', next)
})

const refValue = computed({
  get: () =>
    props.modelValue === null || props.modelValue === undefined
      ? null
      : String(props.modelValue),
  set: (next: string | null) =>
    emit('update:modelValue', next === null ? null : Number(next))
})
</script>

<template>
  <div class="space-y-1">
    <span class="text-default-700 text-sm font-medium">
      {{ label }}
      <span v-if="props.required" class="text-danger-500">*</span>
    </span>
    <EditVocabSelect
      v-if="slot"
      :model-value="props.modelValue"
      :vocab="slot"
      :nullable="!props.required"
      :aria-label="label"
      size="md"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <EditRefPicker
      v-else-if="refFamily"
      v-model="refValue"
      :family="refFamily"
      :aria-label="label"
    />
    <KunNumberInput
      v-else-if="props.field.field_type === 'int'"
      v-model="numberValue"
      :aria-label="label"
    />
    <KunInput v-else v-model="textValue" :aria-label="label" />
    <p v-if="props.config?.help" class="text-default-400 text-xs">
      {{ props.config.help }}
    </p>
  </div>
</template>
