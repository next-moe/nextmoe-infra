<script setup lang="ts">
const props = defineProps<{ fieldKey: string; multiline?: boolean }>()

const { value, disabled, config, field } = useEditorField(() => props.fieldKey)

const text = computed({
  get: () => (typeof value.value === 'string' ? value.value : ''),
  set: (next: string | number) => {
    value.value =
      field.value?.nullable && String(next) === '' ? null : String(next)
  }
})
</script>

<template>
  <KunTextarea
    v-if="props.multiline"
    v-model="text"
    :aria-label="config?.label ?? props.fieldKey"
    :disabled="disabled"
    :rows="3"
    auto-grow
    max-height="512px"
  />
  <KunInput
    v-else
    v-model="text"
    :aria-label="config?.label ?? props.fieldKey"
    :disabled="disabled"
  />
</template>
