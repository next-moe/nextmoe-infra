<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { value, disabled, config } = useEditorField(() => props.fieldKey)

const number = computed({
  get: () =>
    value.value === null || value.value === undefined
      ? null
      : Number(value.value),
  set: (next: number | null) => {
    value.value = next
  }
})
</script>

<template>
  <KunNumberInput
    v-model="number"
    :aria-label="config?.label ?? props.fieldKey"
    :disabled="disabled"
    class="max-w-xs"
  />
</template>
