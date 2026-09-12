<script setup lang="ts">
defineProps<{ client: OAuthClientCreated | null }>()

const open = defineModel<boolean>('open', { required: true })
</script>

<template>
  <KunModal v-model="open" :is-dismissable="false" aria-label="客户端创建成功">
    <div class="space-y-4">
      <div class="flex items-center gap-3">
        <div class="flex size-10 items-center justify-center rounded-full bg-success-100">
          <KunIcon name="lucide:check" class="size-5 text-success" />
        </div>
        <h2 class="text-xl font-bold text-foreground">客户端创建成功</h2>
      </div>

      <div class="rounded-lg bg-warning-50 p-3 text-sm text-warning">
        <KunIcon name="lucide:triangle-alert" class="mr-1 inline size-4" />
        请立即保存以下密钥，关闭后将无法再次查看。
      </div>

      <div class="space-y-3">
        <div class="rounded-lg bg-default-50 p-3">
          <div class="flex items-center justify-between">
            <p class="text-xs text-default-400">Client ID</p>
            <KunCopy :text="client?.id ?? ''" size="sm" />
          </div>
          <p class="mt-1 break-all font-mono text-sm text-foreground">{{ client?.id }}</p>
        </div>

        <div class="rounded-lg bg-default-50 p-3">
          <div class="flex items-center justify-between">
            <p class="text-xs text-default-400">Client Secret</p>
            <KunCopy :text="client?.secret ?? ''" size="sm" />
          </div>
          <p class="mt-1 break-all font-mono text-sm text-foreground">{{ client?.secret }}</p>
        </div>
      </div>

      <div class="flex justify-end">
        <KunButton color="primary" @click="open = false">
          我已保存，关闭
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>
