<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { field, value, disabled, config } = useEditorField(() => props.fieldKey)

const TRISTATE_OPTIONS = [
  { value: 'yes', label: '是' },
  { value: 'no', label: '否' },
  { value: 'unknown', label: '未知' }
] as const

type Tristate = (typeof TRISTATE_OPTIONS)[number]['value']

const checked = computed({
  get: () => value.value === true,
  set: (next: boolean) => {
    value.value = next
  }
})

const tristate = computed<Tristate>({
  get: () =>
    value.value === true ? 'yes' : value.value === false ? 'no' : 'unknown',
  set: (next) => {
    value.value = next === 'unknown' ? null : next === 'yes'
  }
})
</script>

<template>
  <KunRadioGroup
    v-if="field?.nullable"
    v-model="tristate"
    :options="TRISTATE_OPTIONS"
    variant="pill"
    orientation="horizontal"
    size="sm"
    :disabled="disabled"
    :aria-label="config?.label ?? props.fieldKey"
  />
  <KunSwitch
    v-else
    v-model="checked"
    :label="checked ? '是' : '否'"
    :disabled="disabled"
    :aria-label="config?.label ?? props.fieldKey"
  />
</template>
