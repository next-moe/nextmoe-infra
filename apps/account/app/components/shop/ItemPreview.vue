<script setup lang="ts">
const props = defineProps<{
  item: ShopItem | undefined
  userName: string
  avatar: string
  decoration?: 'hover' | 'always'
}>()

const kind = computed(() => props.item?.kind ?? 'avatar_frame')
</script>

<template>
  <div
    v-if="kind === 'profile_background'"
    class="bg-content1 shadow-kun-sm w-full max-w-80 overflow-hidden rounded-xl"
  >
    <ShopBanner :decoration="item?.preview" />
    <div class="flex items-end gap-2 px-3 pb-3">
      <KunAvatar
        :user="{ id: 0, name: userName, avatar }"
        size="md"
        :is-navigation="false"
        class="ring-content1 -mt-5 rounded-full ring-2"
      />
      <span class="text-foreground truncate text-xs font-medium">
        {{ userName }}
      </span>
    </div>
  </div>

  <div
    v-else-if="kind === 'profile_about'"
    class="bg-content1 shadow-kun-sm w-56 space-y-3 rounded-xl p-4"
  >
    <div class="flex items-center gap-2">
      <KunAvatar
        :user="{ id: 0, name: userName, avatar }"
        size="sm"
        :is-navigation="false"
      />
      <span class="text-foreground truncate text-xs font-semibold">
        {{ userName }}
      </span>
      <KunIcon
        name="lucide:notebook-pen"
        class="text-primary-500 ml-auto size-4 shrink-0"
      />
    </div>
    <div class="space-y-1.5">
      <p class="text-foreground text-xs font-semibold">关于我</p>
      <div class="bg-default-200 h-1.5 w-full rounded-full" />
      <div class="bg-default-200 h-1.5 w-11/12 rounded-full" />
      <div class="bg-default-200 h-1.5 w-3/5 rounded-full" />
    </div>
  </div>

  <ShopCoupon v-else-if="kind === 'redeem_code'" :name="item?.name ?? ''" />

  <KunAvatar
    v-else
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
</template>
