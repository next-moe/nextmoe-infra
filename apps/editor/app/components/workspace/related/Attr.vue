<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = withDefaults(
  defineProps<{ label: string; wide?: boolean; hint?: string }>(),
  { wide: false, hint: '' }
)
</script>

<template>
  <div :class="cn('min-w-0 space-y-1.5', props.wide && 'sm:col-span-2')">
    <p class="text-default-500 flex items-center gap-1 text-xs">
      {{ props.label }}
      <KunTooltip v-if="props.hint" :text="props.hint" position="top">
        <KunIcon name="lucide:circle-help" class="text-default-400 size-3.5" />
      </KunTooltip>
    </p>
    <slot />
  </div>
</template>
