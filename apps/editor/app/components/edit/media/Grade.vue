<script setup lang="ts">
import { vocabLabel } from '~/constants/vocabLabels'

const AXES = {
  sexual: {
    label: '性暗示',
    tokens: ['safe', 'suggestive', 'explicit'],
    hints: ['没有性暗示', '擦边、性感但不露骨', '露骨的性内容']
  },
  violence: {
    label: '暴力',
    tokens: ['tame', 'violent', 'brutal'],
    hints: ['没有暴力内容', '有打斗或轻度血腥', '强烈的血腥或残酷画面']
  }
} as const

const props = withDefaults(
  defineProps<{
    axis: keyof typeof AXES
    disabled?: boolean
    changed?: boolean
  }>(),
  { disabled: false, changed: false }
)

const value = defineModel<number>({ required: true })

const { byName } = useVocabularies()

const axis = computed(() => AXES[props.axis])
const options = computed(() => {
  const vocab = byName.value[props.axis]
  const tokens = vocab?.values.length
    ? vocab.values.map((entry) => entry.value)
    : [...axis.value.tokens]
  return tokens.map((token, index) => ({
    value: index,
    label: vocabLabel(props.axis, token, vocab?.values[index]?.display_name)
  }))
})
const hint = computed(() => axis.value.hints[value.value] ?? '')
</script>

<template>
  <div class="space-y-1.5">
    <div class="flex items-center gap-1.5 text-sm">
      <span class="text-default-600">{{ axis.label }}</span>
      <span
        v-if="props.changed"
        class="bg-primary-500 size-1.5 rounded-full"
        aria-label="已修改"
      />
    </div>
    <KunRadioGroup
      v-model="value"
      :options="options"
      variant="pill"
      orientation="horizontal"
      size="sm"
      :disabled="props.disabled"
      :aria-label="axis.label"
    />
    <p class="text-default-500 text-xs">{{ hint }}</p>
  </div>
</template>
