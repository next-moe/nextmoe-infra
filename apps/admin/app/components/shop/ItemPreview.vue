<script setup lang="ts">
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
