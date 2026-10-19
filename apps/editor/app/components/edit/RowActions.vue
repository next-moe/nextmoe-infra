<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    index: number
    count: number
    disabled?: boolean
    removable?: boolean
  }>(),
  { disabled: false, removable: true }
)

const emit = defineEmits<{ move: [from: number, to: number]; remove: [] }>()
</script>

<template>
  <div class="flex shrink-0 items-center gap-0.5">
    <KunButton
      is-icon-only
      size="xs"
      variant="light"
      color="default"
      :disabled="props.disabled || props.index === 0"
      aria-label="上移"
      @click="emit('move', props.index, props.index - 1)"
    >
      <KunIcon name="lucide:arrow-up" class="size-3.5" />
    </KunButton>
    <KunButton
      is-icon-only
      size="xs"
      variant="light"
      color="default"
      :disabled="props.disabled || props.index >= props.count - 1"
      aria-label="下移"
      @click="emit('move', props.index, props.index + 1)"
    >
      <KunIcon name="lucide:arrow-down" class="size-3.5" />
    </KunButton>
    <KunButton
      v-if="props.removable"
      is-icon-only
      size="xs"
      variant="light"
      color="danger"
      :disabled="props.disabled"
      aria-label="删除"
      @click="emit('remove')"
    >
      <KunIcon name="lucide:trash-2" class="size-3.5" />
    </KunButton>
  </div>
</template>
