<script setup lang="ts">
defineProps<{ title: string; icon: string }>()
</script>

<template>
  <section class="space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-foreground flex items-center gap-2 text-lg font-semibold">
        <KunIcon :name="icon" class="text-primary size-5" />
        {{ title }}
      </h2>
      <slot name="action" />
    </div>
    <slot />
  </section>
</template>
