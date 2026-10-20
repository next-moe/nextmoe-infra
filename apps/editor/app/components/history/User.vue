<script setup lang="ts">
import type { CatalogUserRef } from '#shared/types/catalog'

const props = withDefaults(
  defineProps<{
    user?: CatalogUserRef | null
    uid?: string | number | null
    size?: 'xs' | 'sm'
  }>(),
  { user: null, uid: null, size: 'xs' }
)

const chipUser = computed(() => userChipOf(props.user))
</script>

<template>
  <KunUserChip
    v-if="chipUser"
    :user="chipUser"
    :size="props.size"
    :is-navigation="false"
    class-name="text-default-600 align-middle"
  />
  <span v-else class="text-default-600 inline-flex items-center gap-1">
    <KunIcon name="lucide:user-round" class="size-3.5" />
    <template v-if="props.uid">用户 #{{ props.uid }}</template>
    <template v-else>系统</template>
  </span>
</template>
