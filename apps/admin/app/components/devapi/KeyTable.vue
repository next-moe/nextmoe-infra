<script setup lang="ts">
import type { DevKey } from '~~/shared/types/devapi'

defineProps<{ keys: DevKey[]; busy?: boolean }>()
defineEmits<{ rotate: [DevKey]; revoke: [DevKey]; delete: [DevKey] }>()

type KeyStatus = { label: string; color: 'success' | 'warning' | 'danger' | 'default' }

const keyStatus = (k: DevKey): KeyStatus => {
  if (k.revoked_at) return { label: '已吊销', color: 'danger' }
  if (k.expires_at) {
    const expMs = new Date(k.expires_at).getTime()
    if (expMs <= Date.now()) return { label: '已过期', color: 'default' }
    return { label: '宽限中', color: 'warning' }
  }
  return { label: '活跃', color: 'success' }
}

const isRevoked = (k: DevKey) => !!k.revoked_at
const isDeletable = (k: DevKey) => !!k.revoked_at && !k.last_used_at
</script>

<template>
  <div class="space-y-3">
    <KunCard
      v-for="k in keys"
      :key="k.id"
      content-class="justify-start gap-0"
      class-name="p-4"
    >
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-medium text-foreground">{{ k.name }}</span>
            <KunChip :color="keyStatus(k).color" variant="flat" size="xs">
              {{ keyStatus(k).label }}
            </KunChip>
          </div>
          <p class="mt-1 font-mono text-sm text-default-500">
            {{ k.key_prefix }}…{{ k.last4 }}
          </p>
          <div class="mt-2 flex flex-wrap gap-1">
            <KunChip
              v-for="s in k.scopes"
              :key="s"
              color="default"
              variant="flat"
              size="xs"
            >
              {{ s }}
            </KunChip>
          </div>
        </div>

        <div class="flex shrink-0 gap-1">
          <KunButton
            variant="flat"
            size="sm"
            :disabled="busy || isRevoked(k)"
            @click="$emit('rotate', k)"
          >
            <KunIcon name="lucide:refresh-cw" class="mr-1 size-4" />
            轮换
          </KunButton>
          <KunButton
            color="danger"
            variant="flat"
            size="sm"
            :disabled="busy || isRevoked(k)"
            @click="$emit('revoke', k)"
          >
            <KunIcon name="lucide:ban" class="mr-1 size-4" />
            吊销
          </KunButton>
          <KunButton
            v-if="isDeletable(k)"
            color="danger"
            size="sm"
            :disabled="busy"
            @click="$emit('delete', k)"
          >
            <KunIcon name="lucide:trash-2" class="mr-1 size-4" />
            删除
          </KunButton>
        </div>
      </div>

      <div class="mt-3 grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
        <div>
          <p class="text-default-400">创建</p>
          <p class="mt-0.5 text-default-500">
            {{ formatDate(k.created_at, { isShowYear: true, isPrecise: true }) }}
          </p>
        </div>
        <div>
          <p class="text-default-400">最近使用</p>
          <p class="mt-0.5 text-default-500">
            {{ k.last_used_at ? formatDate(k.last_used_at, { isShowYear: true, isPrecise: true }) : '从未' }}
          </p>
        </div>
        <div v-if="k.expires_at">
          <p class="text-default-400">失效时间</p>
          <p class="mt-0.5 text-default-500">
            {{ formatDate(k.expires_at, { isShowYear: true, isPrecise: true }) }}
          </p>
        </div>
        <div v-if="k.revoked_at">
          <p class="text-default-400">吊销时间</p>
          <p class="mt-0.5 text-danger-600">
            {{ formatDate(k.revoked_at, { isShowYear: true, isPrecise: true }) }}
          </p>
        </div>
      </div>
    </KunCard>

    <KunCard v-if="!keys.length" content-class="p-10">
      <p class="text-center text-default-400">该应用尚无密钥</p>
    </KunCard>
  </div>
</template>
