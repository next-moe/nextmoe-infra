<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { MERGE_SIDE_LABELS } from '~/constants/merge'

const props = withDefaults(
  defineProps<{
    side: 'source' | 'target' | 'result'
    plain?: boolean
    className?: string
  }>(),
  { plain: false, className: '' }
)

const DOT = {
  source: 'bg-warning-500',
  target: 'bg-success-500',
  result: 'bg-primary-500'
}
</script>

<template>
  <div
    :class="
      cn(
        'min-w-0',
        !props.plain && 'border-default-200 bg-content1 rounded-xl border',
        props.side === 'result' &&
          !props.plain &&
          'border-primary-200 bg-primary-50/40',
        props.className
      )
    "
  >
    <p
      class="text-default-500 flex items-center gap-1.5 px-3 pt-2.5 text-xs md:hidden"
    >
      <span :class="cn('size-1.5 rounded-full', DOT[props.side])" />
      {{ MERGE_SIDE_LABELS[props.side] }}
    </p>
    <slot />
  </div>
</template>
