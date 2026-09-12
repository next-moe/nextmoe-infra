<script setup lang="ts">
defineProps<{ minted: DevKeyMinted | null; rotated?: boolean }>()

const open = defineModel<boolean>('open', { required: true })
</script>

<template>
  <KunModal
    v-model="open"
    :is-dismissable="false"
    :aria-label="rotated ? '密钥已轮换' : '密钥已生成'"
  >
    <div class="space-y-4">
      <div class="flex items-center gap-3">
        <div class="flex size-10 items-center justify-center rounded-full bg-success-100">
          <KunIcon name="lucide:key-round" class="size-5 text-success" />
        </div>
        <h2 class="text-xl font-bold text-foreground">
          {{ rotated ? '密钥已轮换' : '密钥已生成' }}
        </h2>
      </div>

      <div class="rounded-lg bg-warning-50 p-3 text-sm text-warning">
        <KunIcon name="lucide:triangle-alert" class="mr-1 inline size-4" />
        请立即复制并妥善保存以下密钥，<strong>关闭后将无法再次查看</strong>。
        <template v-if="rotated">旧密钥将在 72 小时宽限后失效。</template>
      </div>

      <div class="rounded-lg bg-default-50 p-3">
        <div class="flex items-center justify-between">
          <p class="text-xs text-default-400">API Key（{{ minted?.name }}）</p>
          <KunCopy :text="minted?.key ?? ''" size="sm" />
        </div>
        <p class="mt-1 break-all font-mono text-sm text-foreground">{{ minted?.key }}</p>
      </div>

      <div class="flex justify-end">
        <KunButton color="primary" @click="open = false">
          我已保存，关闭
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
