<script setup lang="ts">
import type { EditVocabSlot } from '#shared/utils/editModel'
import { vocabLabel } from '~/constants/vocabLabels'
import { INLINE_FIELD_CLASS } from '~/utils/inlineField'

const NONE = '__none__'

const props = withDefaults(
  defineProps<{
    vocab: EditVocabSlot
    modelValue: unknown
    nullable?: boolean
    disabled?: boolean
    ariaLabel?: string
    size?: 'sm' | 'md'
    inline?: boolean
    placeholder?: string
  }>(),
  {
    nullable: false,
    disabled: false,
    ariaLabel: '',
    size: 'sm',
    inline: false,
    placeholder: '请选择'
  }
)

const emit = defineEmits<{ 'update:modelValue': [value: unknown] }>()

const { byName } = useVocabularies()

const options = computed(() => {
  const vocab = byName.value[props.vocab.vocabulary]
  const entries = vocab
    ? vocabWireValues(props.vocab, vocab).map(({ token, wire }, index) => ({
        value: String(wire),
        label: vocabLabel(
          props.vocab.vocabulary,
          token,
          vocab.values[index]?.display_name
        )
      }))
    : []
  return props.nullable
    ? [{ value: NONE, label: '（未设置）' }, ...entries]
    : entries
})

const model = computed({
  get: () =>
    props.modelValue === null ||
    props.modelValue === undefined ||
    props.modelValue === ''
      ? props.nullable
        ? NONE
        : null
      : String(props.modelValue),
  set: (value: string | null) => {
    if (value === null || value === NONE) {
      emit('update:modelValue', null)
      return
    }
    emit(
      'update:modelValue',
      props.vocab.encoding === 'int' ? Number(value) : value
    )
  }
})
</script>

<template>
  <KunSelect
    v-model="model"
    :options="options"
    :aria-label="props.ariaLabel"
    :disabled="props.disabled"
    :searchable="options.length > 12"
    :size="props.size"
    :placeholder="props.placeholder"
    :class-names="props.inline ? { trigger: INLINE_FIELD_CLASS } : undefined"
  />
</template>
