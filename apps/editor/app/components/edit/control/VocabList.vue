<script setup lang="ts">
import { vocabLabel } from '~/constants/vocabLabels'
import type { MergedRow } from '~/utils/mergedRows'

const props = defineProps<{ fieldKey: string }>()

const list = useMergedRows(() => props.fieldKey)
const { config, field, disabled, rows } = list
const { byName } = useVocabularies()

const vocabularyName = computed(
  () => config.value?.vocabulary ?? field.value?.vocabulary ?? ''
)
const entries = computed(() => byName.value[vocabularyName.value]?.values ?? [])
const items = computed(() => list.draftList.value.map(String))
const cap = computed(() => field.value?.max_elements ?? 0)

const labelOfToken = (token: string) => {
  const entry = entries.value.find((e) => e.value === token)
  return vocabLabel(vocabularyName.value, token, entry?.display_name)
}
const labelOf = (row: MergedRow) =>
  row.lane === 'ghost'
    ? list.ghostLabel(row)
    : labelOfToken(
        row.lane === 'human'
          ? String(row.value)
          : (row.upstream?.match ?? row.upstream?.label ?? '')
      )

const present = computed(
  () =>
    new Set([
      ...items.value,
      ...rows.value
        .filter((row) => row.lane === 'upstream')
        .map((row) => row.upstream?.match ?? '')
    ])
)
const options = computed(() =>
  entries.value
    .filter((entry) => !present.value.has(entry.value))
    .map((entry) => ({
      value: entry.value,
      label: vocabLabel(vocabularyName.value, entry.value, entry.display_name)
    }))
)

const showHidden = ref(false)
const visibleRows = computed(() =>
  rows.value.filter((row) => showHidden.value || row.status !== 'hidden')
)
const hiddenCount = computed(
  () => rows.value.filter((row) => row.status === 'hidden').length
)

const pick = ref<string | null>(null)
watch(pick, (token) => {
  if (!token) {
    return
  }
  list.append(token)
  pick.value = null
})
</script>

<template>
  <div class="space-y-3">
    <ul v-if="visibleRows.length" class="flex flex-wrap gap-2">
      <li v-for="row in visibleRows" :key="row.key" class="max-w-full">
        <EditListChip
          :row="row"
          :label="labelOf(row)"
          :source="sourceLabelOf(row)"
          :can-hide="list.canHide(row)"
          :can-remove="!disabled"
          @remove="list.removeAt(row.index)"
          @hide="list.hide(row)"
          @restore="list.restore(row)"
        />
      </li>
    </ul>
    <p v-else class="text-default-400 text-sm">未设置。</p>
    <EditListHiddenToggle v-model="showHidden" :count="hiddenCount" />
    <div v-if="!disabled" class="flex items-center gap-2">
      <KunSelect
        v-model="pick"
        class="w-64"
        :options="options"
        :searchable="options.length > 12"
        :disabled="cap > 0 && items.length >= cap"
        size="sm"
        :aria-label="`添加${config?.label ?? ''}`"
        :placeholder="`添加${config?.label ?? ''}…`"
      />
      <span v-if="cap" class="text-default-400 text-xs tabular-nums">
        人工 {{ items.length }} / {{ cap }}
      </span>
    </div>
  </div>
</template>
