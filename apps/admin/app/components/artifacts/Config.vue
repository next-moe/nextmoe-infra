<script setup lang="ts">
import type { OAuthClient } from '~~/shared/types/oauth-client'

const { data: clients, status, refresh } =
  await useApiFetch<OAuthClient[]>('/oauth/clients')

const editing = ref<OAuthClient | null>(null)
const configOpen = ref(false)

const openConfig = (c: OAuthClient) => {
  editing.value = c
  configOpen.value = true
}

const onUpdated = async () => {
  configOpen.value = false
  await refresh()
  useKunMessage('存储配置已更新', 'success')
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-foreground text-2xl font-bold">存储配置</h1>
    <p class="text-default-500 text-sm">
      管理各 OAuth 客户端的 Artifact / Image 上传能力与配额。启用某项能力等同于
      为该客户端开通对应服务（原先只能改 SQL）。
    </p>

    <div v-if="status === 'pending'" class="text-default-400 py-8 text-center">
      加载中…
    </div>

    <div v-else class="space-y-3">
      <KunCard
        v-for="c in clients ?? []"
        :key="c.id"
        content-class="justify-start"
        class-name="p-4"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <div class="text-foreground font-semibold">{{ c.name }}</div>
            <div class="text-default-400 truncate font-mono text-xs">
              {{ c.id }}
            </div>
            <div class="mt-2 flex flex-wrap items-center gap-2">
              <KunChip
                :color="c.storage?.artifact_enabled ? 'success' : 'default'"
                variant="flat"
                size="xs"
              >
                Artifact {{ c.storage?.artifact_enabled ? '已启用' : '未启用' }}
              </KunChip>
              <KunChip
                v-if="c.storage?.artifact_enabled"
                color="default"
                variant="flat"
                size="xs"
              >
                site: {{ c.storage?.artifact_site_key || '—' }}
              </KunChip>
              <KunChip
                :color="c.storage?.image_enabled ? 'success' : 'default'"
                variant="flat"
                size="xs"
              >
                Image {{ c.storage?.image_enabled ? '已启用' : '未启用' }}
              </KunChip>
              <KunChip
                v-if="c.storage?.image_enabled"
                color="default"
                variant="flat"
                size="xs"
              >
                site: {{ c.storage?.image_site_key || '—' }}
              </KunChip>
            </div>
          </div>
          <KunButton
            color="primary"
            variant="flat"
            size="sm"
            class-name="shrink-0"
            @click="openConfig(c)"
          >
            <KunIcon name="lucide:sliders-horizontal" class="mr-1 size-4" />
            配置
          </KunButton>
        </div>
      </KunCard>
    </div>

    <ArtifactsConfigModal
      v-model:open="configOpen"
      :client="editing"
      @updated="onUpdated"
    />
  </div>
</template>
