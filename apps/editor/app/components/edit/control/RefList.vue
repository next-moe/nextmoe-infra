<script setup lang="ts">
import type { MergedRow } from '~/utils/mergedRows'

const props = defineProps<{ fieldKey: string }>()

const list = useMergedRows(() => props.fieldKey)
const { context, config, field, disabled, rows, summary } = list
const refs = context.refs

const family = computed(() => config.value?.ref)
const ids = computed(() => list.draftList.value.map(String))
const cap = computed(() => field.value?.max_elements ?? 0)

const showHidden = ref(false)
const visibleRows = computed(() =>
  rows.value.filter((row) => showHidden.value || row.status !== 'hidden')
)
const hiddenCount = computed(
  () => rows.value.filter((row) => row.status === 'hidden').length
)
const excluded = computed(() => [
  ...ids.value,
  ...rows.value
    .filter((row) => row.lane === 'upstream' && row.upstream?.match)
    .map((row) => row.upstream!.match!)
])

const labelOf = (row: MergedRow) => {
  if (row.lane === 'ghost') {
    return list.ghostLabel(row)
  }
  if (!family.value) {
    return ''
  }
  const id = row.lane === 'human' ? String(row.value) : row.upstream?.match
  return (
    (id ? refs.nameOf(family.value, id) : '') ||
    row.upstream?.label ||
    (id ? `#${id}` : '')
  )
}

const reorder = useListReorder(
  () => list.draftList.value,
  (next) => list.setDraft(next)
)

const pickerKey = ref(0)
const add = (id: string | null) => {
  if (!id || ids.value.includes(id)) {
    return
  }
  const hiddenMatch = rows.value.find(
    (row) => row.lane === 'upstream' && row.upstream?.match === id
  )
  if (hiddenMatch && hiddenMatch.status !== 'kept') {
    list.restore(hiddenMatch)
  } else {
    list.append(Number(id))
  }
  pickerKey.value++
}

watch(
  ids,
  (next) => {
    if (family.value) {
      refs.resolve(family.value, next)
    }
  },
  { immediate: true }
)
</script>

<template>
  <div v-if="family" class="space-y-3">
    <ul v-if="visibleRows.length" class="flex flex-wrap gap-2">
      <li
        v-for="row in visibleRows"
        :key="row.key"
        class="max-w-full"
        v-bind="
          row.lane === 'human' && row.status !== 'removed' && !disabled
            ? reorder.dropAttrs(row.index)
            : {}
        "
      >
        <EditListChip
          :row="row"
          :label="labelOf(row)"
          :source="sourceLabelOf(row)"
          :can-hide="list.canHide(row)"
          :can-remove="!disabled"
          @remove="list.removeAt(row.index)"
          @hide="list.hide(row)"
          @restore="list.restore(row)"
        >
          <template
            v-if="row.lane === 'human' && row.status !== 'removed' && !disabled"
            #lead
          >
            <span
              v-bind="reorder.handleAttrs(row.index)"
              class="text-default-300 hover:text-default-500 -ml-1 cursor-grab"
              aria-hidden="true"
            >
              <KunIcon name="lucide:grip-vertical" class="size-3.5" />
            </span>
          </template>
        </EditListChip>
      </li>
    </ul>
    <p v-else class="text-default-400 text-sm">还没有任何条目。</p>

    <EditListHiddenToggle v-model="showHidden" :count="hiddenCount" />

    <div v-if="!disabled" class="flex flex-wrap items-center gap-2">
      <EditRefPicker
        :key="pickerKey"
        :family="family"
        :exclude="excluded"
        :disabled="cap > 0 && ids.length >= cap"
        :placeholder="`添加${config?.label ?? ''}：搜索名称或输入 ID`"
        class="w-full max-w-md"
        @update:model-value="add"
      />
      <span class="text-default-400 text-xs tabular-nums">
        {{ summary.visible }} 项
        <template v-if="summary.upstream">
          · {{ summary.upstream }} 项来自资料源
        </template>
      </span>
    </div>
  </div>
  <EditControlReadonly v-else :field-key="props.fieldKey" />
</template>
