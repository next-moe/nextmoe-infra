<script setup lang="ts">
const props = defineProps<{
  uid: string | number | null | undefined
}>()
</script>

<template>
  <span class="text-default-600 inline-flex items-center gap-1">
    <KunIcon name="lucide:user-round" class="size-3.5" />
    <template v-if="props.uid">用户 #{{ props.uid }}</template>
    <template v-else>系统</template>
  </span>
</template>
