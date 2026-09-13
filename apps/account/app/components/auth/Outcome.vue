<script setup lang="ts">
withDefaults(
  defineProps<{
    icon: string
    title: string
    description?: string
    tone?: 'success' | 'danger'
  }>(),
  { description: '', tone: 'success' }
)
</script>

<template>
  <div class="mb-8">
    <span
      class="mb-6 inline-flex size-14 items-center justify-center rounded-2xl"
      :class="tone === 'success' ? 'bg-success-50' : 'bg-danger-50'"
    >
      <KunIcon
        :name="icon"
        class="size-7"
        :class="tone === 'success' ? 'text-success' : 'text-danger'"
      />
    </span>
    <h1
      class="text-foreground text-[1.75rem] leading-tight font-semibold tracking-tight"
    >
      {{ title }}
    </h1>
    <p v-if="description" class="text-default-500 mt-3 text-sm leading-relaxed">
      {{ description }}
    </p>
  </div>
</template>
