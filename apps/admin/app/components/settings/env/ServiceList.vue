<script setup lang="ts">
import type { EnvServiceView } from '~~/shared/types/settings'

defineProps<{ services: EnvServiceView[] }>()

const formatTime = (iso: string) => new Date(iso).toLocaleString()
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <div class="flex flex-wrap items-baseline gap-2">
      <h2 class="text-foreground text-lg font-bold">服务</h2>
      <span class="text-default-500 text-sm">
        每个服务启动时上报它实际读到的全部变量;下表即来自这些上报
      </span>
    </div>

    <p v-if="!services.length" class="text-default-400 text-sm">
      还没有服务上报过;服务启动后这里会出现它们
    </p>

    <div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
      <div
        v-for="svc in services"
        :key="svc.name"
        class="border-default-200 rounded-lg border p-3"
      >
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-foreground font-mono font-medium">
            {{ svc.name }}
          </span>
          <KunChip
            :color="svc.store_loaded ? 'success' : 'danger'"
            variant="flat"
            size="xs"
          >
            {{ svc.store_loaded ? '已读取配置中心' : '未读取配置中心' }}
          </KunChip>
        </div>
        <p class="text-default-400 mt-1 text-xs">
          启动于 {{ formatTime(svc.started_at) }} · {{ svc.hostname }}
        </p>
        <div v-if="svc.unread.length" class="text-warning-600 mt-1 text-xs">
          <KunTooltip
            text="容器拿到了这些变量,但没有任何代码读取:多半是消费者已删除或变量名拼错"
            position="top"
          >
            <span>{{ svc.unread.length }} 个变量没人读取:</span>
          </KunTooltip>
          <span class="font-mono break-all">{{ svc.unread.join(', ') }}</span>
        </div>
      </div>
    </div>
  </KunCard>
</template>
