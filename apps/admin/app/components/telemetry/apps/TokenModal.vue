<script setup lang="ts">
defineProps<{ token: string }>()
const open = defineModel<boolean>({ required: true })
</script>

<template>
  <KunModal v-model="open" title="CI 符号上传令牌" size="md">
    <div class="space-y-4">
      <p class="text-warning-600 text-sm">
        只显示这一次，请立即配置到 CI 的 TELEMETRY_SYMBOLS_TOKEN
      </p>
      <div class="flex items-start gap-2">
        <p class="text-foreground min-w-0 flex-1 font-mono text-sm break-all">
          {{ token }}
        </p>
        <KunCopy :text="token" name="复制" size="sm" />
      </div>
      <div class="flex justify-end">
        <KunButton color="primary" @click="open = false">已保存</KunButton>
      </div>
    </div>
  </KunModal>
</template>
