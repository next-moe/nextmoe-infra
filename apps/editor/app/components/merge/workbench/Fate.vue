<script setup lang="ts">
import { MERGE_FATE } from '~/constants/merge'

const props = withDefaults(defineProps<{ fate: string; reason?: string }>(), {
  reason: ''
})

const meta = computed(
  () =>
    MERGE_FATE[props.fate] ?? {
      label: props.fate,
      color: 'default' as const,
      hint: ''
    }
)
</script>

<template>
  <WorkspaceHint :text="props.reason || meta.hint" position="top">
    <KunChip size="xs" variant="flat" :color="meta.color" class-name="shrink-0">
      {{ meta.label }}
    </KunChip>
  </WorkspaceHint>
</template>
