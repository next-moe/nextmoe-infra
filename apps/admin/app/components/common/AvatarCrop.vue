<script setup lang="ts">
import { cn } from '@kungal/ui-core'

defineProps<{ disabled?: boolean }>()
const emit = defineEmits<{ crop: [blob: Blob] }>()
</script>

<template>
  <div :class="cn(disabled && 'pointer-events-none opacity-50')">
    <KunUpload
      :size="512"
      :aspect="1"
      description="点击或拖拽选择图片，可裁剪为正方形"
      class-name="mx-auto w-48"
      @set-image="emit('crop', $event)"
    />
  </div>
</template>
