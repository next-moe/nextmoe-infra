<script setup lang="ts">
import { vocabLabel } from '~/constants/vocabLabels'

const props = defineProps<{ fieldKey: string }>()

const { value, disabled, config, field } = useEditorField(() => props.fieldKey)
const { byName } = useVocabularies()

const vocabularyName = computed(
  () => config.value?.vocabulary ?? field.value?.vocabulary ?? ''
)
const entries = computed(() => byName.value[vocabularyName.value]?.values ?? [])

const items = computed<string[]>(() =>
  Array.isArray(value.value) ? value.value.map(String) : []
)
const cap = computed(() => field.value?.max_elements ?? 0)

const labelOf = (token: string) => {
  const entry = entries.value.find((e) => e.value === token)
  return vocabLabel(vocabularyName.value, token, entry?.display_name)
}

const options = computed(() =>
  entries.value
    .filter((entry) => !items.value.includes(entry.value))
    .map((entry) => ({
      value: entry.value,
      label: vocabLabel(vocabularyName.value, entry.value, entry.display_name)
    }))
)

const pick = ref<string | null>(null)
watch(pick, (token) => {
  if (!token) {
    return
  }
  value.value = [...items.value, token]
  pick.value = null
})

const remove = (token: string) => {
  value.value = items.value.filter((item) => item !== token)
}
</script>

<template>
  <div class="space-y-2">
    <div v-if="items.length" class="flex flex-wrap gap-2">
      <KunChip
        v-for="token in items"
        :key="token"
        size="sm"
        variant="flat"
        :closable="!disabled"
        @close="remove(token)"
      >
        {{ labelOf(token) }}
      </KunChip>
    </div>
    <p v-else class="text-default-400 text-sm">未设置。</p>
    <div v-if="!disabled" class="flex items-center gap-2">
      <KunSelect
        v-model="pick"
        class="w-60"
        :options="options"
        :searchable="options.length > 12"
        :disabled="cap > 0 && items.length >= cap"
        size="sm"
        aria-label="添加"
        placeholder="添加…"
      />
      <span v-if="cap" class="text-default-400 text-xs tabular-nums">
        {{ items.length }} / {{ cap }}
      </span>
    </div>
    <EditUpstream :field-key="props.fieldKey" :exclude="items" />
    <EditSuppressedList :field-key="props.fieldKey" />
  </div>
</template>
