<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { SHOP_STAGE_TINT } from '~/constants/shop'

const props = defineProps<{
  item: ShopItem | undefined
  userName: string
  avatar: string
  decoration?: 'hover' | 'always'
  className?: string
}>()

const tint = computed(() => SHOP_STAGE_TINT[props.item?.kind ?? 'avatar_frame'])
</script>

<template>
  <div
    :class="
      cn('relative flex items-center justify-center px-5 py-6', tint, className)
    "
  >
    <ShopItemPreview
      :item="item"
      :user-name="userName"
      :avatar="avatar"
      :decoration="decoration"
    />
    <slot />
  </div>
</template>
