<script setup lang="ts">
import { ENTITY_FAMILY } from '~/constants/catalog'

const props = defineProps<{
  family: string
  id: string | number
  name: string
}>()

const family = computed(() => ENTITY_FAMILY[props.family])
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-1.5">
    <span v-if="family" class="text-default-400 shrink-0 text-xs">{{
      family.label
    }}</span>
    <NuxtLink
      v-if="family"
      :to="`${family.path}/${props.id}`"
      class="hover:text-primary min-w-0 truncate font-medium transition-colors"
    >
      {{ props.name }}
    </NuxtLink>
    <span v-else class="min-w-0 truncate font-medium">{{ props.name }}</span>
  </span>
</template>
