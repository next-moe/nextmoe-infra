<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const { value, disabled, config, field } = useEditorField(() => props.fieldKey)

const items = computed<string[]>(() =>
  Array.isArray(value.value) ? value.value.map(String) : []
)
const cap = computed(() => field.value?.max_elements ?? 0)

const update = (next: string[]) => {
  value.value = next
}
const reorder = useListReorder(() => items.value, update)

const setAt = (index: number, next: string | number) => {
  update(items.value.map((item, i) => (i === index ? String(next) : item)))
}

const draftEntry = ref('')
const add = () => {
  const entry = draftEntry.value.trim()
  if (!entry) {
    return
  }
  update([...items.value, entry])
  draftEntry.value = ''
}
</script>

<template>
  <div>
    <ul class="space-y-2">
      <li
        v-for="(item, index) in items"
        :key="index"
        v-bind="disabled ? {} : reorder.dropAttrs(index)"
        class="flex items-center gap-2"
      >
        <EditDragHandle
          v-if="!disabled"
          v-bind="reorder.handleAttrs(index)"
          @keydown="reorder.onKeydown($event, index)"
        />
        <KunInput
          :model-value="item"
          :aria-label="`${config?.label ?? '条目'} ${index + 1}`"
          :disabled="disabled"
          class="min-w-0 flex-1"
          @update:model-value="setAt(index, $event)"
        />
        <a
          v-if="/^https?:\/\//.test(item)"
          :href="item"
          target="_blank"
          rel="noopener noreferrer"
          class="text-default-400 hover:text-primary shrink-0"
          aria-label="打开链接"
        >
          <KunIcon name="lucide:external-link" class="size-4" />
        </a>
        <EditRowActions
          :index="index"
          :count="items.length"
          :disabled="disabled"
          @move="reorder.move"
          @remove="update(items.filter((_, i) => i !== index))"
        />
      </li>
    </ul>
    <form
      v-if="!disabled && (!cap || items.length < cap)"
      class="mt-3 flex items-center gap-2"
      @submit.prevent="add"
    >
      <KunInput
        v-model="draftEntry"
        placeholder="https://"
        :aria-label="`添加${config?.label ?? '条目'}`"
        class="min-w-0 flex-1"
      />
      <KunButton type="submit" variant="flat" :disabled="!draftEntry.trim()">
        <KunIcon name="lucide:plus" class="size-4" />
        添加
      </KunButton>
    </form>

    <EditUpstream :field-key="props.fieldKey" :exclude="items" />
    <EditSuppressedList :field-key="props.fieldKey" />
  </div>
</template>
