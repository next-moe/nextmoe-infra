<script setup lang="ts">
import { STORE_RANGE_PRESETS } from '~/constants/store'

const range = defineModel<[string, string]>({ required: true })

const picked = computed<[string | null, string | null]>({
  get: () => range.value,
  set: ([from, to]) => {
    if (from && to) range.value = [from, to]
  },
})

const isActive = (preset: (typeof STORE_RANGE_PRESETS)[number]) => {
  const [from, to] = preset.range()
  return range.value[0] === from && range.value[1] === to
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <KunButton
      v-for="preset in STORE_RANGE_PRESETS"
      :key="preset.id"
      size="sm"
      :color="isActive(preset) ? 'primary' : 'default'"
      :variant="isActive(preset) ? 'solid' : 'flat'"
      @click="range = preset.range()"
    >
      {{ preset.label }}
    </KunButton>
    <KunDatePicker v-model="picked" mode="range" class-name="w-64" aria-label="统计区间" />
  </div>
</template>
