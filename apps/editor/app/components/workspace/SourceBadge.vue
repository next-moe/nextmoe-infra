<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = withDefaults(
  defineProps<{
    source?: string | null
    human?: boolean
    machine?: boolean
    className?: string
  }>(),
  { source: null, human: false, machine: false, className: '' }
)

const text = computed(() => (props.human ? '人工' : props.source || '资料源'))
</script>

<template>
  <span :class="cn('inline-flex shrink-0 items-center gap-1', props.className)">
    <span
      :class="
        cn(
          'inline-flex h-5 items-center rounded px-1.5 text-[11px] leading-none font-medium',
          props.human
            ? 'bg-secondary-100 text-secondary-700'
            : 'bg-default-100 text-default-600'
        )
      "
    >
      {{ text }}
    </span>
    <span
      v-if="props.machine"
      class="bg-warning-100 text-warning-700 inline-flex h-5 items-center rounded px-1.5 text-[11px] leading-none font-medium"
    >
      机翻
    </span>
  </span>
</template>
