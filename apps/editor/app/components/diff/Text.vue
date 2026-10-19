<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { DiffSegment } from '~/utils/review/textDiff'

const props = defineProps<{
  segments: DiffSegment[]
  side?: 'both' | 'before' | 'after'
}>()

const visible = computed(() =>
  props.segments.filter((segment) => {
    if (props.side === 'before') return segment.op !== 'add'
    if (props.side === 'after') return segment.op !== 'del'
    return true
  })
)
</script>

<template>
  <span class="break-words whitespace-pre-wrap">
    <span
      v-for="(segment, index) in visible"
      :key="index"
      :class="
        cn(
          segment.op === 'add' && 'bg-success-100 text-success-800 rounded-sm',
          segment.op === 'del' &&
            'bg-danger-100 text-danger-800 rounded-sm line-through'
        )
      "
      >{{ segment.text }}</span
    >
  </span>
</template>
