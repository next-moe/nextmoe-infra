<script setup lang="ts">
import { MERGE_SECTIONS, type MergeSectionKey } from '~/constants/merge'

const props = defineProps<{ sectionKey: MergeSectionKey }>()

const def = computed(() =>
  MERGE_SECTIONS.find((section) => section.key === props.sectionKey)!
)
</script>

<template>
  <section
    :id="`merge-section-${props.sectionKey}`"
    class="scroll-mt-16 space-y-3"
    :aria-labelledby="`merge-section-${props.sectionKey}-title`"
  >
    <div class="flex flex-wrap items-end justify-between gap-2">
      <div class="min-w-0">
        <h2
          :id="`merge-section-${props.sectionKey}-title`"
          class="flex items-center gap-2 text-base font-semibold"
        >
          <KunIcon :name="def.icon" class="text-default-500 size-4" />
          {{ def.label }}
        </h2>
        <p class="text-default-500 mt-0.5 text-sm">{{ def.description }}</p>
      </div>
      <div class="flex items-center gap-2">
        <slot name="actions" />
      </div>
    </div>
    <slot />
  </section>
</template>
