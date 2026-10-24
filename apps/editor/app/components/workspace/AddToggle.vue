<script setup lang="ts">
const props = defineProps<{ label: string }>()

const adding = defineModel<boolean>({ default: false })
</script>

<template>
  <KunButton
    size="sm"
    :variant="adding ? 'flat' : 'light'"
    :color="adding ? 'default' : 'primary'"
    @click="adding = !adding"
  >
    <KunIcon :name="adding ? 'lucide:x' : 'lucide:plus'" class="size-4" />
    {{ adding ? '取消' : props.label }}
  </KunButton>
</template>
