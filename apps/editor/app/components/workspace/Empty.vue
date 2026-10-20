<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = withDefaults(
  defineProps<{
    icon?: string
    title: string
    description?: string
    className?: string
  }>(),
  { icon: 'lucide:inbox', description: '', className: '' }
)
</script>

<template>
  <div
    :class="
      cn(
        'flex flex-col items-center justify-center gap-2 px-6 py-10 text-center',
        props.className
      )
    "
  >
    <div
      class="bg-default-100 text-default-400 flex size-10 items-center justify-center rounded-full"
    >
      <KunIcon :name="props.icon" class="size-5" />
    </div>
    <p class="text-foreground text-sm font-medium">{{ props.title }}</p>
    <p
      v-if="props.description"
      class="text-default-500 max-w-sm text-xs leading-5"
    >
      {{ props.description }}
    </p>
    <div v-if="$slots.default" class="mt-2 flex flex-wrap justify-center gap-2">
      <slot />
    </div>
  </div>
</template>
