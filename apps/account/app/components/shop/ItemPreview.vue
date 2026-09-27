<script setup lang="ts">
import { SHOP_KIND_ICON } from '~/constants/shop'

const props = defineProps<{
  item: ShopItem | undefined
  userName: string
  avatar: string
  decoration?: 'hover' | 'always'
}>()

const icon = computed(() => props.item && SHOP_KIND_ICON[props.item.kind])
</script>

<template>
  <ShopBanner
    v-if="item?.kind === 'profile_background'"
    :decoration="item.preview"
    class-name="rounded-lg"
  />
  <div v-else-if="icon" class="flex justify-center">
    <div
      class="bg-primary-50 text-primary-600 flex size-24 items-center justify-center rounded-2xl"
    >
      <KunIcon :name="icon" class="size-10" />
    </div>
  </div>
  <div v-else class="flex justify-center">
    <KunAvatar
      :user="{
        id: 0,
        name: userName,
        avatar,
        avatarDecoration: toAvatarDecoration(item?.preview)
      }"
      size="original-sm"
      :decoration="decoration ?? 'hover'"
      :is-navigation="false"
    />
  </div>
</template>
