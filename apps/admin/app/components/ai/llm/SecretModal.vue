<script setup lang="ts">
defineProps<{ secret: string }>()
const open = defineModel<boolean>({ required: true })
</script>

<template>
  <KunModal
    v-model="open"
    title="访问密钥已创建"
    size="md"
    :is-dismissable="false"
  >
    <div class="space-y-4">
      <div
        class="border-warning-200 bg-warning-50 text-warning-700 rounded-lg border p-3 text-sm"
      >
        密钥只显示这一次,关闭后无法再查看。请立即复制并保存到调用方的配置中;丢失只能删除后重建。
      </div>
      <div class="flex items-start gap-2">
        <p class="text-foreground min-w-0 flex-1 font-mono text-sm break-all">
          {{ secret }}
        </p>
        <KunCopy :text="secret" name="复制" size="sm" />
      </div>
      <div class="flex justify-end">
        <KunButton color="primary" @click="open = false">我已保存</KunButton>
      </div>
    </div>
  </KunModal>
</template>
