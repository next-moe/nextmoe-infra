<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { MergedRow } from '~/utils/mergedRows'

const props = withDefaults(
  defineProps<{
    row: MergedRow
    source?: string
    machine?: boolean
    canHide?: boolean
    canRemove?: boolean
    lockReason?: string
    removeLabel?: string
    className?: string
    trailClassName?: string
    hideSource?: boolean
  }>(),
  {
    source: '',
    machine: false,
    canHide: false,
    canRemove: false,
    lockReason: '',
    removeLabel: '删除',
    className: '',
    trailClassName: '',
    hideSource: false
  }
)

const emit = defineEmits<{
  remove: []
  hide: []
  restore: []
  undo: []
}>()

const hidden = computed(
  () => props.row.status === 'hidden' || props.row.status === 'hiding'
)
const removed = computed(() => props.row.status === 'removed')
const marked = computed(() =>
  ['added', 'changed', 'removed', 'hiding', 'unhiding'].includes(
    props.row.status
  )
)

const STATUS_TEXT: Partial<Record<MergedRow['status'], string>> = {
  added: '新增',
  changed: '已修改',
  removed: '将删除',
  hiding: '将隐藏',
  unhiding: '将恢复',
  hidden: '已隐藏'
}
</script>

<template>
  <div
    :class="
      cn(
        'group relative flex min-h-11 items-center gap-2 rounded-lg px-2 py-1.5 transition-colors',
        props.row.status === 'added' && 'bg-success-50',
        props.row.status === 'removed' && 'bg-danger-50',
        !marked && !hidden && 'hover:bg-default-50',
        marked &&
          'before:absolute before:inset-y-1.5 before:left-0 before:w-0.5 before:rounded-full',
        props.row.status === 'added' && 'before:bg-success-500',
        props.row.status === 'removed' && 'before:bg-danger-500',
        (props.row.status === 'changed' ||
          props.row.status === 'hiding' ||
          props.row.status === 'unhiding') &&
          'before:bg-primary-500',
        props.className
      )
    "
  >
    <slot name="lead" />
    <div
      :class="
        cn(
          'min-w-0 flex-1',
          hidden && 'opacity-50',
          removed && 'text-default-500 line-through'
        )
      "
    >
      <slot />
    </div>

    <div
      :class="cn('flex shrink-0 items-center gap-1.5', props.trailClassName)"
    >
      <span
        v-if="STATUS_TEXT[props.row.status]"
        :class="
          cn(
            'hidden text-xs whitespace-nowrap sm:inline',
            props.row.status === 'added' && 'text-success-700',
            props.row.status === 'removed' && 'text-danger-700',
            props.row.status === 'hidden' && 'text-default-400',
            ['changed', 'hiding', 'unhiding'].includes(props.row.status) &&
              'text-primary-600'
          )
        "
      >
        {{ STATUS_TEXT[props.row.status] }}
      </span>
      <KunIcon
        v-if="hidden"
        name="lucide:eye-off"
        class="text-default-400 size-3.5"
      />
      <template v-if="props.hideSource" />
      <WorkspaceSourceBadge v-else-if="props.row.lane === 'human'" human />
      <WorkspaceSourceBadge
        v-else-if="props.row.lane === 'upstream'"
        :source="props.source"
        :machine="props.machine"
      />
      <span
        v-else-if="props.row.lane === 'ghost'"
        class="text-default-400 text-xs"
      >
        资料源已移除
      </span>

      <div
        :class="
          cn(
            'flex items-center gap-1',
            props.row.status === 'kept' &&
              'sm:opacity-0 sm:transition-opacity sm:group-focus-within:opacity-100 sm:group-hover:opacity-100'
          )
        "
      >
        <slot name="actions" />
      </div>

      <template v-if="removed">
        <KunButton
          size="xs"
          variant="light"
          color="default"
          @click="emit('restore')"
        >
          <KunIcon name="lucide:undo-2" class="size-3.5" />
          撤销删除
        </KunButton>
      </template>
      <template v-else-if="hidden">
        <KunButton
          v-if="props.canHide"
          size="xs"
          variant="light"
          color="primary"
          @click="emit('restore')"
        >
          <KunIcon name="lucide:eye" class="size-3.5" />
          恢复
        </KunButton>
      </template>
      <div
        v-else
        :class="
          cn(
            'flex items-center',
            props.row.status === 'kept' &&
              'sm:opacity-0 sm:transition-opacity sm:group-focus-within:opacity-100 sm:group-hover:opacity-100'
          )
        "
      >
        <KunTooltip
          v-if="
            props.row.status === 'unhiding' || props.row.status === 'changed'
          "
          text="撤销这一行的修改"
          position="top"
        >
          <KunButton
            size="xs"
            variant="light"
            color="default"
            is-icon-only
            aria-label="撤销这一行的修改"
            @click="emit('undo')"
          >
            <KunIcon name="lucide:undo-2" class="size-3.5" />
          </KunButton>
        </KunTooltip>
        <KunTooltip
          v-if="props.row.lane === 'human' && props.canRemove"
          :text="props.removeLabel"
          position="top"
        >
          <KunButton
            size="xs"
            variant="light"
            color="danger"
            is-icon-only
            :aria-label="props.removeLabel"
            @click="emit('remove')"
          >
            <KunIcon name="lucide:trash-2" class="size-3.5" />
          </KunButton>
        </KunTooltip>
        <KunTooltip
          v-else-if="props.row.lane === 'upstream' && props.canHide"
          text="隐藏这条资料源记录：所有页面都不再显示，资料源再次同步也不会加回"
          position="top"
        >
          <KunButton
            size="xs"
            variant="light"
            color="default"
            is-icon-only
            aria-label="隐藏"
            @click="emit('hide')"
          >
            <KunIcon name="lucide:eye-off" class="size-3.5" />
          </KunButton>
        </KunTooltip>
        <KunTooltip
          v-else-if="props.row.lane === 'upstream' && props.lockReason"
          :text="props.lockReason"
          position="top"
        >
          <span
            class="text-default-300 inline-flex size-7 items-center justify-center"
          >
            <KunIcon name="lucide:lock" class="size-3.5" />
          </span>
        </KunTooltip>
      </div>
    </div>
  </div>
</template>
