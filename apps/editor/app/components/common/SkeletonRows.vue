<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = withDefaults(
  defineProps<{
    rows?: number
    avatar?: 'none' | 'circle' | 'cover'
    meta?: boolean
    trailing?: boolean
    className?: string
  }>(),
  { rows: 5, avatar: 'none', meta: true, trailing: false, className: '' }
)

const WIDTHS = ['72%', '58%', '84%', '64%', '48%', '76%']
</script>

<template>
  <ul
    :class="cn('divide-default-200 divide-y', props.className)"
    aria-busy="true"
    aria-label="正在加载"
  >
    <li
      v-for="index in props.rows"
      :key="index"
      class="flex items-center gap-3 py-3"
    >
      <KunSkeleton
        v-if="props.avatar === 'circle'"
        variant="circle"
        height="2rem"
      />
      <KunSkeleton
        v-else-if="props.avatar === 'cover'"
        width="2.5rem"
        height="3.25rem"
        rounded="md"
      />
      <div class="min-w-0 flex-1">
        <KunSkeleton
          variant="text"
          class-name="text-sm"
          :width="WIDTHS[index % WIDTHS.length]"
        />
        <KunSkeleton
          v-if="props.meta"
          variant="text"
          class-name="text-xs"
          :width="index % 2 ? '40%' : '28%'"
        />
      </div>
      <KunSkeleton
        v-if="props.trailing"
        width="3.5rem"
        height="1.5rem"
        rounded="full"
      />
    </li>
  </ul>
</template>
