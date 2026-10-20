<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    options: { value: string | number; label: string }[]
    label: string
    disabled?: boolean
  }>(),
  { disabled: false }
)

const model = defineModel<string | number | null>({ default: null })

const selected = computed({
  get: () => model.value ?? '',
  set: (next: string | number) => {
    model.value = next
  }
})
</script>

<template>
  <KunRadioGroup
    v-model="selected"
    :options="props.options"
    :aria-label="props.label"
    variant="pill"
    orientation="horizontal"
    size="sm"
    :disabled="props.disabled"
  />
</template>
