<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { value, disabled, config, field } = useEditorField(() => props.fieldKey)

const vocabSlot = computed(() =>
  field.value ? fieldVocabSlot(field.value) : null
)
</script>

<template>
  <EditVocabSelect
    v-if="vocabSlot"
    v-model="value"
    :vocab="vocabSlot"
    :nullable="field?.nullable"
    :disabled="disabled"
    :aria-label="config?.label ?? props.fieldKey"
    size="md"
    class="max-w-sm"
  />
</template>
