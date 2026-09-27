<script setup lang="ts">
import { SHOP_KINDS } from '~/constants/shop'

const props = defineProps<{
  kind: ShopKind
  staticUrl?: string
  animatedUrl?: string
  animate?: boolean
}>()

const bannerSrc = computed(() =>
  props.animate && props.animatedUrl
    ? props.animatedUrl
    : (props.staticUrl ?? '')
)
const icon = computed(() => SHOP_KINDS[props.kind]?.icon)
</script>

<template>
  <div
    v-if="kind === 'profile_background'"
    class="bg-default-100 aspect-[3/1] w-full overflow-hidden rounded-lg"
  >
    <KunImage
      v-if="bannerSrc"
      :src="bannerSrc"
      alt=""
      class-name="size-full"
      object-fit="cover"
    />
  </div>
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
        name: '预览',
        avatar: '',
        avatarDecoration: staticUrl
          ? { src: staticUrl, animatedSrc: animatedUrl || undefined }
          : null
      }"
      size="original-sm"
      :decoration="animate ? 'always' : 'hover'"
      :is-navigation="false"
    />
  </div>
</template>
