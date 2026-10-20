<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = withDefaults(
  defineProps<{
    title: string
    description?: string
    count?: number | null
    changes?: number
    className?: string
  }>(),
  { description: '', count: null, changes: 0, className: '' }
)
</script>

<template>
  <header
    :class="
      cn(
        'flex flex-wrap items-start justify-between gap-x-4 gap-y-2',
        props.className
      )
    "
  >
    <div class="min-w-0">
      <h2 class="flex items-center gap-2 text-lg font-semibold">
        {{ props.title }}
        <span
          v-if="props.count !== null"
          class="text-default-400 text-sm font-normal tabular-nums"
        >
          {{ props.count }}
        </span>
        <KunChip v-if="props.changes" size="xs" variant="flat" color="primary">
          {{ props.changes }} 项修改
        </KunChip>
      </h2>
      <p
        v-if="props.description"
        class="text-default-500 mt-1 max-w-3xl text-sm leading-6"
      >
        {{ props.description }}
      </p>
    </div>
    <div v-if="$slots.default" class="flex shrink-0 items-center gap-2">
      <slot />
    </div>
  </header>
</template>
