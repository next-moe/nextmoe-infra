<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { MOEMOEPOINT_ICON } from '~/constants/moemoepoint'

defineProps<{ amount: number; className?: string }>()
</script>

<template>
  <span
    :class="
      cn(
        'text-foreground inline-flex items-center gap-1 font-semibold tabular-nums',
        className
      )
    "
  >
    <KunIcon
      :name="MOEMOEPOINT_ICON"
      class="text-secondary-500 size-[1.05em]"
    />
    {{ amount }}
  </span>
</template>
