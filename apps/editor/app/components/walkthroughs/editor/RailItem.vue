<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = withDefaults(
  defineProps<{
    title: string
    subtitle?: string
    icon?: string
    dot?: string
    issues?: string[]
    expanded?: boolean
    selected?: boolean
  }>(),
  { subtitle: '', icon: '', dot: '', issues: () => [], expanded: false, selected: false }
)

const emit = defineEmits<{ toggle: []; remove: [] }>()
const doc = useWalkthroughDocument()
</script>

<template>
  <div
    :class="
      cn(
        'group/rail rounded-lg transition-colors',
        props.expanded ? 'bg-default-50 ring-default-200 ring-1' : props.selected ? 'bg-primary-50' : 'hover:bg-default-50'
      )
    "
  >
    <div class="flex items-center gap-2 px-2 py-1.5">
      <button
        type="button"
        class="flex min-w-0 flex-1 items-center gap-2 text-left"
        :aria-expanded="props.expanded"
        @click="emit('toggle')"
      >
        <span v-if="props.dot" :class="cn('size-2.5 shrink-0 rounded-full', props.dot)" />
        <KunIcon v-else-if="props.icon" :name="props.icon" class="text-default-400 size-4 shrink-0" />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-sm">{{ props.title }}</span>
          <span v-if="props.subtitle" class="text-default-400 block truncate text-xs">{{ props.subtitle }}</span>
        </span>
        <KunIcon v-if="props.issues.length" name="lucide:circle-alert" class="text-warning-500 size-4 shrink-0" />
      </button>
      <slot name="trailing" />
      <button
        v-if="!doc.readonly.value"
        type="button"
        tabindex="-1"
        class="text-default-300 hover:bg-danger-50 hover:text-danger-600 flex size-6 shrink-0 items-center justify-center rounded opacity-0 group-hover/rail:opacity-100"
        aria-label="删除"
        @click="emit('remove')"
      >
        <KunIcon name="lucide:trash-2" class="size-3.5" />
      </button>
    </div>
    <div v-if="props.expanded" class="space-y-2 px-2 pb-2.5">
      <p v-for="message in props.issues" :key="message" class="text-warning-700 text-xs">{{ message }}</p>
      <slot />
    </div>
  </div>
</template>
