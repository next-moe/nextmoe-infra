<script setup lang="ts">
import { INLINE_FIELD_CLASS } from '~/utils/inlineField'

const props = defineProps<{ fieldKey: string }>()

const list = useMergedRows(() => props.fieldKey)
const { config, field, disabled, rows } = list

const cap = computed(() => field.value?.max_elements ?? 0)
const reorder = useListReorder(
  () => list.draftList.value,
  (next) => list.setDraft(next)
)

const showHidden = ref(false)
const visibleRows = computed(() =>
  rows.value.filter((row) => showHidden.value || row.status !== 'hidden')
)
const hiddenCount = computed(
  () => rows.value.filter((row) => row.status === 'hidden').length
)

const draftEntry = ref('')
const duplicate = computed(() =>
  list.draftList.value.map(String).includes(draftEntry.value.trim())
)
const add = () => {
  const entry = draftEntry.value.trim()
  if (!entry || duplicate.value) {
    return
  }
  list.append(entry)
  draftEntry.value = ''
}
</script>

<template>
  <div class="space-y-2">
    <ul v-if="visibleRows.length" class="space-y-0.5">
      <li
        v-for="row in visibleRows"
        :key="row.key"
        v-bind="
          row.lane === 'human' && row.status !== 'removed' && !disabled
            ? reorder.dropAttrs(row.index)
            : {}
        "
      >
        <EditListRow
          :row="row"
          :source="sourceLabelOf(row)"
          :can-hide="list.canHide(row)"
          :can-remove="!disabled"
          @remove="list.removeAt(row.index)"
          @hide="list.hide(row)"
          @restore="list.restore(row)"
          @undo="list.undo(row)"
        >
          <template v-if="!disabled" #lead>
            <EditDragHandle
              v-if="row.lane === 'human' && row.status !== 'removed'"
              v-bind="reorder.handleAttrs(row.index)"
              @keydown="reorder.onKeydown($event, row.index)"
            />
            <span v-else class="w-5 shrink-0" />
          </template>
          <KunInput
            v-if="row.lane === 'human' && row.status !== 'removed'"
            :model-value="String(row.value ?? '')"
            :aria-label="`${config?.label ?? '条目'} ${row.index + 1}`"
            :disabled="disabled"
            :class-name="INLINE_FIELD_CLASS"
            size="sm"
            @update:model-value="list.setAt(row.index, String($event))"
          />
          <span v-else class="block truncate text-sm leading-8">
            {{
              row.lane === 'ghost'
                ? list.ghostLabel(row)
                : String(row.value ?? row.upstream?.label ?? '')
            }}
          </span>
        </EditListRow>
      </li>
    </ul>
    <p v-else class="text-default-400 px-2 text-sm">还没有任何条目。</p>
    <EditListHiddenToggle v-model="showHidden" :count="hiddenCount" />
    <form
      v-if="!disabled && (!cap || list.draftList.value.length < cap)"
      class="flex items-center gap-2 px-2"
      @submit.prevent="add"
    >
      <KunInput
        v-model="draftEntry"
        :placeholder="`添加${config?.label ?? '条目'}`"
        :aria-label="`添加${config?.label ?? '条目'}`"
        :error="duplicate ? '已经存在' : ''"
        class="min-w-0 flex-1"
        size="sm"
      />
      <KunButton
        type="submit"
        variant="flat"
        size="sm"
        :disabled="!draftEntry.trim() || duplicate"
      >
        <KunIcon name="lucide:plus" class="size-4" />
        添加
      </KunButton>
    </form>
  </div>
</template>
