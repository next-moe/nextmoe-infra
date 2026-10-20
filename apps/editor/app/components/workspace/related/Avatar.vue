<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { RelatedPicture } from '~/composables/useWorkRelated'

const props = withDefaults(
  defineProps<{
    picture?: RelatedPicture | null
    icon?: string
    shape?: 'circle' | 'square' | 'cover'
    size?: 'sm' | 'md' | 'lg'
    className?: string
  }>(),
  {
    picture: null,
    icon: 'lucide:image',
    shape: 'square',
    size: 'md',
    className: ''
  }
)

const box = computed(() => {
  const sizes = {
    circle: { sm: 'size-7', md: 'size-9', lg: 'size-12' },
    square: { sm: 'size-7', md: 'size-9', lg: 'size-12' },
    cover: { sm: 'h-9 w-7', md: 'h-12 w-9', lg: 'h-20 w-15' }
  }
  return sizes[props.shape][props.size]
})
</script>

<template>
  <div
    :class="
      cn(
        'bg-default-100 text-default-400 flex shrink-0 items-center justify-center overflow-hidden',
        props.shape === 'circle' ? 'rounded-full' : 'rounded-md',
        box,
        props.className
      )
    "
  >
    <KunImage
      v-if="props.picture?.url"
      :src="props.picture.url"
      :thumbhash="props.picture.thumbhash ?? undefined"
      alt=""
      object-fit="cover"
      :image-class-name="props.shape === 'circle' ? 'object-top' : ''"
      class-name="h-full w-full"
    />
    <KunIcon v-else :name="props.icon" class="size-4" />
  </div>
</template>
