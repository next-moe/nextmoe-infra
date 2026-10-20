<script setup lang="ts">
const props = defineProps<{ fieldKey: string }>()

const label = computed(() =>
  props.fieldKey.includes('screenshots') ? '截图' : '封面'
)
</script>

<template>
  <p class="text-default-500 text-sm">
    资料源{{ label }}的分级、类型与隐藏在「{{ label }}」分区里逐张编辑。
  </p>
</template>
