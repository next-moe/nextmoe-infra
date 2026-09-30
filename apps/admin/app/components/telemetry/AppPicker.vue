<script setup lang="ts">
defineProps<{
  options: { value: string; label: string }[]
  disabled?: boolean
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <KunSelect
    v-model="model"
    :options="options"
    :disabled="disabled"
    label="应用"
    aria-label="应用"
    :full-width="false"
    class="w-56"
  />
</template>
