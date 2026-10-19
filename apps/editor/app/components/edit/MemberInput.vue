<script setup lang="ts">
import type { CatalogSchemaMember, EditMemberConfig } from '#shared/types/edit'

const props = withDefaults(
  defineProps<{
    member: CatalogSchemaMember
    config?: EditMemberConfig
    modelValue: unknown
    disabled?: boolean
    invalid?: boolean
  }>(),
  { config: undefined, disabled: false, invalid: false }
)

const emit = defineEmits<{ 'update:modelValue': [value: unknown] }>()

const label = computed(() => props.config?.label ?? props.member.key)
const vocabSlot = computed(() => memberVocabSlot(props.member))

const refValue = computed({
  get: () =>
    props.modelValue === null ||
    props.modelValue === undefined ||
    props.modelValue === ''
      ? null
      : String(props.modelValue),
  set: (next: string | null) =>
    emit('update:modelValue', next === null ? null : Number(next))
})

const textValue = computed({
  get: () =>
    props.modelValue === null || props.modelValue === undefined
      ? ''
      : String(props.modelValue),
  set: (next: string | number) => emit('update:modelValue', String(next))
})

const numberValue = computed({
  get: () =>
    props.modelValue === null ||
    props.modelValue === undefined ||
    props.modelValue === ''
      ? null
      : Number(props.modelValue),
  set: (next: number | null) => emit('update:modelValue', next)
})

const boolValue = computed({
  get: () => props.modelValue === true,
  set: (next: boolean) => emit('update:modelValue', next)
})
</script>

<template>
  <EditRefPicker
    v-if="props.member.type === 'ref' && props.config?.ref"
    v-model="refValue"
    :family="props.config.ref"
    :disabled="props.disabled"
    :aria-label="label"
    :placeholder="props.config.placeholder"
  />
  <EditVocabSelect
    v-else-if="vocabSlot"
    :model-value="props.modelValue"
    :vocab="vocabSlot"
    :nullable="props.member.nullable"
    :disabled="props.disabled"
    :aria-label="label"
    @update:model-value="emit('update:modelValue', $event)"
  />
  <KunSwitch
    v-else-if="props.member.type === 'bool'"
    v-model="boolValue"
    :disabled="props.disabled"
    :aria-label="label"
    size="sm"
  />
  <KunNumberInput
    v-else-if="props.member.type === 'int' || props.member.type === 'ref'"
    v-model="numberValue"
    :disabled="props.disabled"
    :aria-label="label"
    :is-invalid="props.invalid"
    size="sm"
  />
  <span
    v-else-if="props.member.type === 'imagehash'"
    class="text-default-500 block truncate font-mono text-xs"
    :title="textValue"
  >
    {{ textValue }}
  </span>
  <KunInput
    v-else
    v-model="textValue"
    :disabled="props.disabled"
    :aria-label="label"
    :placeholder="props.config?.placeholder"
    :is-invalid="props.invalid"
    size="sm"
  />
</template>
