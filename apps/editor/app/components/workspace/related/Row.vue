<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { RelatedPicture } from '~/composables/useWorkRelated'
import type { RelatedState } from '~/utils/relatedRows'

const props = withDefaults(
  defineProps<{
    title: string
    subtitle?: string
    selected?: boolean
    state?: RelatedState
    picture?: RelatedPicture | null
    icon?: string
    shape?: 'circle' | 'square' | 'cover'
    source?: string
    human?: boolean
  }>(),
  {
    subtitle: '',
    selected: false,
    state: null,
    picture: null,
    icon: 'lucide:circle',
    shape: 'square',
    source: '',
    human: false
  }
)

const emit = defineEmits<{ select: [] }>()

const STATE = {
  added: { text: '新增', className: 'bg-success-100 text-success-700' },
  changed: { text: '已修改', className: 'bg-primary-100 text-primary-700' },
  hidden: { text: '已隐藏', className: 'bg-default-100 text-default-500' }
} as const
</script>

<template>
  <button
    type="button"
    :aria-current="props.selected ? 'true' : undefined"
    :class="
      cn(
        'group relative flex w-full items-center gap-3 px-3 py-2 text-left transition-colors',
        props.selected
          ? 'bg-primary-50'
          : 'hover:bg-default-100 focus-visible:bg-default-100',
        props.state === 'hidden' && 'opacity-60'
      )
    "
    @click="emit('select')"
  >
    <span
      v-if="props.selected"
      class="bg-primary-500 absolute inset-y-1.5 left-0 w-0.5 rounded-full"
    />
    <WorkspaceRelatedAvatar
      :picture="props.picture"
      :icon="props.icon"
      :shape="props.shape"
    />
    <span class="min-w-0 flex-1">
      <span class="flex items-center gap-1.5">
        <span
          :class="
            cn(
              'truncate text-sm',
              props.selected ? 'text-primary-700 font-semibold' : 'font-medium',
              props.state === 'hidden' && 'line-through'
            )
          "
        >
          {{ props.title }}
        </span>
        <span
          v-if="props.state"
          :class="
            cn(
              'shrink-0 rounded px-1 text-[10px] leading-4 font-medium',
              STATE[props.state].className
            )
          "
        >
          {{ STATE[props.state].text }}
        </span>
      </span>
      <span
        v-if="props.subtitle || $slots.meta"
        class="text-default-500 mt-0.5 flex min-w-0 items-center gap-1.5 text-xs"
      >
        <slot name="meta">
          <span class="truncate">{{ props.subtitle }}</span>
        </slot>
      </span>
    </span>
    <span class="flex shrink-0 items-center gap-1">
      <slot name="trailing" />
      <WorkspaceSourceBadge
        v-if="props.source || props.human"
        :source="props.source"
        :human="props.human"
      />
    </span>
  </button>
</template>
