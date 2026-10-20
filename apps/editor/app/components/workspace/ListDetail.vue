<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    hasSelection: boolean
    listLabel?: string
    minSize?: number
    maxSize?: number
  }>(),
  { listLabel: '列表', minSize: 260, maxSize: 520 }
)

const emit = defineEmits<{ back: [] }>()

const SIZE_KEY = 'nextmoe-editor-list-width'
const size = ref(340)
onMounted(() => {
  try {
    const stored = Number(localStorage.getItem(SIZE_KEY))
    if (stored >= props.minSize && stored <= props.maxSize) {
      size.value = stored
    }
  } catch {
    size.value = 340
  }
})
const remember = (next: number) => {
  try {
    localStorage.setItem(SIZE_KEY, String(next))
  } catch {
    return
  }
}
</script>

<template>
  <div class="h-full min-h-0">
    <KunSplitPane
      v-model:size="size"
      :min-size="props.minSize"
      :max-size="props.maxSize"
      stack-below="lg"
      :show-pane="props.hasSelection ? 'end' : 'start'"
      :aria-label="`调整${props.listLabel}宽度`"
      @resize-end="remember"
    >
      <template #start>
        <div class="bg-content1 flex h-full min-h-0 flex-col">
          <div
            v-if="$slots.header"
            class="border-default-200 shrink-0 border-b px-3 py-2.5"
          >
            <slot name="header" />
          </div>
          <div class="min-h-0 flex-1 overflow-y-auto">
            <slot name="list" />
          </div>
          <div
            v-if="$slots.footer"
            class="border-default-200 shrink-0 border-t px-3 py-2"
          >
            <slot name="footer" />
          </div>
        </div>
      </template>
      <template #end>
        <div class="bg-content1 flex h-full min-h-0 flex-col">
          <div
            v-if="props.hasSelection"
            class="border-default-200 shrink-0 border-b px-3 py-2 lg:hidden"
          >
            <KunButton variant="light" size="sm" @click="emit('back')">
              <KunIcon name="lucide:chevron-left" class="size-4" />
              返回{{ props.listLabel }}
            </KunButton>
          </div>
          <div class="min-h-0 flex-1">
            <slot v-if="props.hasSelection" name="detail" />
            <slot v-else name="empty">
              <WorkspaceEmpty
                icon="lucide:mouse-pointer-click"
                title="从左侧选择一项"
                description="选中后在这里查看和编辑，修改会和其他条目的修改一起提交。"
                class-name="h-full"
              />
            </slot>
          </div>
        </div>
      </template>
    </KunSplitPane>
  </div>
</template>
