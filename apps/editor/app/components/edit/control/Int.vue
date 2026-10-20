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
  <div class="max-w-56">
    <KunNumberInput
      v-model="number"
      :aria-label="config?.label ?? props.fieldKey"
      :disabled="disabled"
    />
  </div>
</template>
