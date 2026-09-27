<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'

defineProps<{
  title: string
  hint?: string
  to?: RouteLocationRaw
  count: number
}>()
</script>

<template>
  <section class="space-y-2">
    <header v-if="title" class="flex items-baseline gap-2 px-1">
      <NuxtLink
        v-if="to"
        :to="to"
        class="text-foreground hover:text-primary truncate font-medium"
      >
        {{ title }}
      </NuxtLink>
      <span v-else class="text-foreground truncate font-medium">{{
        title
      }}</span>
      <span v-if="hint" class="text-default-400 truncate text-sm">{{
        hint
      }}</span>
      <span class="text-default-400 ml-auto shrink-0 text-xs tabular-nums">{{
        count
      }}</span>
    </header>
    <div class="space-y-2">
      <slot />
    </div>
  </section>
</template>
