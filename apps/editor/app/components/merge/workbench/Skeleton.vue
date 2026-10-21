<script setup lang="ts">
const props = withDefaults(defineProps<{ compact?: boolean }>(), {
  compact: false
})
</script>

<template>
  <div class="space-y-6" aria-busy="true" aria-label="加载中">
    <div v-if="!props.compact" class="grid gap-3 md:grid-cols-[1fr_auto_1fr]">
      <KunSkeleton variant="rect" height="7rem" class-name="rounded-xl" />
      <span class="hidden w-8 md:block" />
      <KunSkeleton variant="rect" height="7rem" class-name="rounded-xl" />
    </div>
    <div v-for="row in 3" :key="row" class="space-y-2">
      <KunSkeleton variant="text" width="8rem" />
      <div class="grid gap-3 md:grid-cols-3">
        <KunSkeleton
          v-for="col in 3"
          :key="col"
          variant="rect"
          height="5.5rem"
          class-name="rounded-xl"
        />
      </div>
    </div>
  </div>
</template>
