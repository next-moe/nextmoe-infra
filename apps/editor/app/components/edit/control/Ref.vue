<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { value, disabled, config } = useEditorField(() => props.fieldKey)

const selected = computed({
  get: () =>
    value.value === null || value.value === undefined
      ? null
      : String(value.value),
  set: (next: string | null) => {
    value.value = next === null ? null : Number(next)
  }
})
</script>

<template>
  <EditRefPicker
    v-if="config?.ref"
    v-model="selected"
    :family="config.ref"
    :disabled="disabled"
    :aria-label="config.label"
    class="max-w-md"
  />
  <EditControlReadonly v-else :field-key="props.fieldKey" />
</template>
