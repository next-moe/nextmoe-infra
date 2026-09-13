<script setup lang="ts">
defineProps<{
  title: string
  subtitle?: string
}>()
</script>

<template>
  <div class="mb-9">
    <h1
      class="text-foreground 3xl:text-[2.125rem] text-[1.75rem] leading-tight font-semibold tracking-tight"
    >
      {{ title }}
    </h1>
    <p v-if="subtitle" class="text-default-500 3xl:text-base mt-2.5 text-sm">
      {{ subtitle }}
    </p>
  </div>
</template>
