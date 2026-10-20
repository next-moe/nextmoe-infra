<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { MergedRow } from '~/utils/mergedRows'

const props = withDefaults(
  defineProps<{
    row: MergedRow
    label: string
    source?: string
    canHide?: boolean
    canRemove?: boolean
  }>(),
  { source: '', canHide: false, canRemove: false }
)

const emit = defineEmits<{ remove: []; hide: []; restore: [] }>()

const hidden = computed(
  () => props.row.status === 'hidden' || props.row.status === 'hiding'
)
const removed = computed(() => props.row.status === 'removed')

const tooltip = computed(() => {
  const origin =
    props.row.lane === 'human'
      ? '人工添加'
      : props.row.lane === 'ghost'
        ? '资料源已移除这一条，隐藏仍然生效'
        : `来自 ${props.source || '资料源'}`
  const state: Partial<Record<MergedRow['status'], string>> = {
    added: '（新增）',
    removed: '（将删除）',
    hiding: '（将隐藏）',
    hidden: '（已隐藏）',
    unhiding: '（将恢复显示）'
  }
  return `${origin}${state[props.row.status] ?? ''}`
})
</script>

<template>
  <KunTooltip :text="tooltip" position="top">
    <span
      :class="
        cn(
          'inline-flex h-8 max-w-full items-center gap-1.5 rounded-lg border pr-1 pl-2.5 text-sm transition-colors',
          props.row.lane === 'human'
            ? 'border-secondary-200 bg-secondary-50'
            : 'border-default-200 bg-content1',
          props.row.status === 'added' && 'border-success-300 bg-success-50',
          removed && 'border-danger-300 bg-danger-50 text-default-500',
          hidden && 'border-dashed opacity-60',
          (props.row.status === 'hiding' || props.row.status === 'unhiding') &&
            'border-primary-300'
        )
      "
    >
      <slot name="lead" />
      <KunIcon v-if="hidden" name="lucide:eye-off" class="size-3.5 shrink-0" />
      <span :class="cn('truncate', removed && 'line-through')">
        {{ props.label }}
      </span>
      <span
        v-if="props.row.lane === 'upstream' && props.source"
        class="text-default-400 shrink-0 text-[11px]"
      >
        {{ props.source }}
      </span>
      <button
        v-if="removed || hidden"
        type="button"
        class="text-primary-600 hover:bg-primary-100 shrink-0 rounded p-1"
        :aria-label="`恢复 ${props.label}`"
        @click="emit('restore')"
      >
        <KunIcon name="lucide:undo-2" class="size-3.5" />
      </button>
      <button
        v-else-if="props.row.lane === 'human' && props.canRemove"
        type="button"
        class="text-default-400 hover:bg-danger-100 hover:text-danger-600 shrink-0 rounded p-1"
        :aria-label="`移除 ${props.label}`"
        @click="emit('remove')"
      >
        <KunIcon name="lucide:x" class="size-3.5" />
      </button>
      <button
        v-else-if="props.row.lane === 'upstream' && props.canHide"
        type="button"
        class="text-default-400 hover:bg-default-100 hover:text-foreground shrink-0 rounded p-1"
        :aria-label="`隐藏 ${props.label}`"
        @click="emit('hide')"
      >
        <KunIcon name="lucide:eye-off" class="size-3.5" />
      </button>
      <span v-else class="w-1" />
    </span>
  </KunTooltip>
</template>
