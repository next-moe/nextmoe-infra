<script setup lang="ts">
defineProps<{
  item: ShopItem | undefined
  userName: string
  avatar: string
  decoration?: 'hover' | 'always'
}>()
</script>

<template>
  <ShopBanner
    v-if="item?.kind === 'profile_background'"
    :decoration="item.preview"
    class-name="rounded-lg"
  />
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
