<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = defineProps<{ fieldKey: string }>()

const { context, value, baseValue, disabled, config, field } = useEditorField(
  () => props.fieldKey
)
const refs = context.refs

const family = computed(() => config.value?.ref)
const ids = computed<string[]>(() =>
  Array.isArray(value.value) ? value.value.map(String) : []
)
const baseIds = computed(
  () =>
    new Set(Array.isArray(baseValue.value) ? baseValue.value.map(String) : [])
)
const cap = computed(() => field.value?.max_elements ?? 0)

const update = (next: string[]) => {
  value.value = next.map(Number)
}

const reorder = useListReorder(() => ids.value, update)

const pickerKey = ref(0)
const add = (id: string | null) => {
  if (!id || ids.value.includes(id)) {
    return
  }
  update([...ids.value, id])
  pickerKey.value++
}

const remove = (index: number) => {
  update(ids.value.filter((_, i) => i !== index))
}

watch(
  ids,
  (list) => {
    if (family.value) {
      refs.resolve(family.value, list)
    }
  },
  { immediate: true }
)
</script>

<template>
  <div v-if="family">
    <ul v-if="ids.length" class="flex flex-wrap gap-2">
      <li
        v-for="(id, index) in ids"
        :key="id"
        v-bind="disabled ? {} : reorder.dropAttrs(index)"
        :class="
          cn(
            'border-default-200 bg-default-50 flex items-center gap-1.5 rounded-lg border py-1 pr-1 pl-1 text-sm',
            !baseIds.has(id) && 'border-success-300 bg-success-50',
            reorder.overIndex.value === index && 'ring-primary-300 ring-2'
          )
        "
      >
        <EditDragHandle
          v-if="!disabled"
          v-bind="reorder.handleAttrs(index)"
          @keydown="reorder.onKeydown($event, index)"
        />
        <span>{{ refs.nameOf(family, id) || `#${id}` }}</span>
        <span class="text-default-400 font-mono text-xs">#{{ id }}</span>
        <button
          v-if="!disabled"
          type="button"
          class="text-default-400 hover:text-danger-600 rounded p-0.5"
          :aria-label="`移除 ${refs.nameOf(family, id) || id}`"
          @click="remove(index)"
        >
          <KunIcon name="lucide:x" class="size-3.5" />
        </button>
      </li>
    </ul>
    <p v-else class="text-default-400 text-sm">还没有人工添加的条目。</p>

    <div v-if="!disabled" class="mt-3 flex items-center gap-2">
      <EditRefPicker
        :key="pickerKey"
        :family="family"
        :exclude="ids"
        :disabled="cap > 0 && ids.length >= cap"
        class="w-full max-w-md"
        @update:model-value="add"
      />
      <span v-if="cap" class="text-default-400 shrink-0 text-xs tabular-nums">
        {{ ids.length }} / {{ cap }}
      </span>
    </div>
    <p v-if="ids.length > 1 && !disabled" class="text-default-400 mt-1 text-xs">
      拖动或按 Alt + ↑/↓ 调整顺序。
    </p>

    <EditUpstream :field-key="props.fieldKey" :exclude="ids" />
    <EditSuppressedList :field-key="props.fieldKey" />
  </div>
  <EditControlReadonly v-else :field-key="props.fieldKey" />
</template>
