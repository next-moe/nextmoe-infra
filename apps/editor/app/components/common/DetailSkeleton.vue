<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const props = withDefaults(
  defineProps<{
    media?: 'cover' | 'square' | 'none'
    header?: boolean
    tabs?: number
    blocks?: number
  }>(),
  { media: 'square', header: true, tabs: 5, blocks: 2 }
)
</script>

<template>
  <div class="space-y-5" aria-busy="true" aria-label="正在加载">
    <div
      v-if="props.header"
      :class="
        cn(
          'flex gap-4',
          props.media === 'cover'
            ? 'flex-col sm:flex-row sm:gap-5'
            : 'items-center'
        )
      "
    >
      <KunSkeleton
        v-if="props.media === 'cover'"
        height="auto"
        rounded="lg"
        class-name="aspect-[3/4] w-32 rounded-xl sm:w-40"
      />
      <KunSkeleton
        v-else-if="props.media === 'square'"
        rounded="lg"
        width="auto"
        height="auto"
        class-name="size-16 sm:size-20"
      />
      <div class="min-w-0 flex-1 space-y-2">
        <KunSkeleton variant="text" class-name="text-2xl" width="55%" />
        <KunSkeleton variant="text" class-name="text-sm" width="35%" />
        <div v-if="props.media === 'cover'" class="flex gap-1.5 pt-1">
          <KunSkeleton
            v-for="index in 3"
            :key="index"
            width="3.5rem"
            height="1.5rem"
            rounded="full"
          />
        </div>
      </div>
    </div>

    <div
      v-if="props.tabs"
      class="border-default-200 flex gap-5 overflow-hidden border-b pb-2.5"
    >
      <KunSkeleton
        v-for="index in props.tabs"
        :key="index"
        variant="text"
        class-name="text-sm"
        width="3.5rem"
      />
    </div>

    <div
      v-for="index in props.blocks"
      :key="index"
      class="border-default-200 bg-content1 space-y-3 rounded-xl border p-4"
    >
      <KunSkeleton variant="text" class-name="font-semibold" width="8rem" />
      <div>
        <KunSkeleton variant="text" class-name="text-sm" />
        <KunSkeleton variant="text" class-name="text-sm" width="92%" />
        <KunSkeleton variant="text" class-name="text-sm" width="68%" />
      </div>
    </div>
  </div>
</template>
