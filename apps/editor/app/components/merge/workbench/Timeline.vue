<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { TimelineStep } from '~/utils/merge/timeline'

const props = defineProps<{ steps: TimelineStep[]; now: number }>()

const ICON: Record<TimelineStep['state'], string> = {
  done: 'lucide:check',
  current: 'lucide:clock',
  upcoming: 'lucide:circle',
  stopped: 'lucide:x'
}
const CIRCLE: Record<TimelineStep['state'], string> = {
  done: 'bg-success-100 text-success-700',
  current: 'bg-primary-100 text-primary-700 ring-primary-200 ring-4',
  upcoming: 'bg-default-100 text-default-400',
  stopped: 'bg-danger-100 text-danger-700'
}

const when = (step: TimelineStep) => {
  if (!step.at) {
    return ''
  }
  const at = new Date(step.at).getTime()
  if (step.state === 'current' && at > props.now) {
    return `还剩 ${formatWait(at - props.now)}（${formatDateTime(step.at)}）`
  }
  return formatDateTime(step.at)
}
</script>

<template>
  <ol class="grid gap-3 md:auto-cols-fr md:grid-flow-col md:gap-0">
    <li
      v-for="(step, index) in props.steps"
      :key="step.key"
      class="relative flex gap-3 md:flex-col md:gap-2 md:pr-4"
    >
      <div class="flex items-center md:w-full">
        <span
          :class="
            cn(
              'flex size-7 shrink-0 items-center justify-center rounded-full',
              CIRCLE[step.state]
            )
          "
        >
          <KunIcon :name="ICON[step.state]" class="size-4" />
        </span>
        <span
          v-if="index < props.steps.length - 1"
          :class="
            cn(
              'ml-2 hidden h-px flex-1 md:block',
              step.state === 'done' ? 'bg-success-300' : 'bg-default-200'
            )
          "
        />
      </div>
      <div class="min-w-0">
        <p
          :class="
            cn(
              'text-sm font-medium',
              step.state === 'upcoming' && 'text-default-400'
            )
          "
        >
          {{ step.label }}
        </p>
        <p v-if="when(step)" class="text-default-500 text-xs tabular-nums">
          {{ when(step) }}
        </p>
        <p v-if="step.detail" class="text-default-400 text-xs">
          {{ step.detail }}
        </p>
      </div>
    </li>
  </ol>
</template>
